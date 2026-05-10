package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/policy"
)

// reconcileTestStrategy resolves the effective TestPlan for a Preview before tests run.
// Returns (result, plan, error). The caller should run tests only when result is empty
// and err is nil. A nil plan means "skip tests" (fallbackOnAgentTimeout=Skip).
func (r *PreviewReconciler) reconcileTestStrategy(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName string,
) (ctrl.Result, *platformv1alpha1.TestPlan, error) {
	logger := log.FromContext(ctx).WithValues("previewName", preview.Name)
	mode := policy.EffectiveMode(preview)
	correlationID := makeCorrelationID(preview)

	logger.Info("resolving test strategy",
		"mode", mode,
		"correlationID", correlationID,
	)

	switch mode {
	case platformv1alpha1.TestStrategyManual:
		plan, err := r.resolveManualPlan(ctx, preview, nsName, correlationID)
		return ctrl.Result{}, plan, err

	case platformv1alpha1.TestStrategyAuto:
		return r.reconcileAutoPlan(ctx, preview, nsName, correlationID)

	default: // FullSuite or absent
		plan := r.applyFullSuiteFallback(ctx, preview, nsName, correlationID,
			platformv1alpha1.TestPlanSourceFull)
		return ctrl.Result{}, plan, nil
	}
}

// reconcileAutoPlan handles the Auto mode state machine.
func (r *PreviewReconciler) reconcileAutoPlan(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName, correlationID string,
) (ctrl.Result, *platformv1alpha1.TestPlan, error) {
	logger := log.FromContext(ctx).WithValues("previewName", preview.Name)

	existing, err := r.findActiveTestPlan(ctx, preview, nsName)
	if err != nil {
		return ctrl.Result{}, nil, err
	}

	if existing == nil {
		stub, err := r.createStubTestPlanAndReturn(ctx, preview, nsName, correlationID)
		if err != nil {
			return ctrl.Result{}, nil, err
		}
		// Trigger the test-strategist agent asynchronously — it reads the stub,
		// reads the Preview's changeContext + ReconcileEvents, and patches the plan to Ready.
		r.triggerTestStrategistAgent(preview, nsName, stub.Name)
		timeout := time.Duration(policy.AgentTimeoutSeconds(preview)) * time.Second
		preview.Status.Phase = platformv1alpha1.PhaseAwaitingTestPlan
		if serr := r.Status().Update(ctx, preview); serr != nil {
			return ctrl.Result{}, nil, serr
		}
		logger.Info("waiting for agent to fill TestPlan",
			"testPlan", stub.Name,
			"timeoutSeconds", policy.AgentTimeoutSeconds(preview),
		)
		return ctrl.Result{RequeueAfter: timeout / 6}, nil, nil
	}

	switch existing.Status.Phase {
	case platformv1alpha1.TestPlanPhaseReady:
		return r.acceptOrFallback(ctx, preview, nsName, existing, correlationID)

	case platformv1alpha1.TestPlanPhaseStale:
		stub, err := r.createStubTestPlanAndReturn(ctx, preview, nsName, correlationID)
		if err != nil {
			return ctrl.Result{}, nil, err
		}
		r.triggerTestStrategistAgent(preview, nsName, stub.Name)
		timeout := time.Duration(policy.AgentTimeoutSeconds(preview)) * time.Second
		preview.Status.Phase = platformv1alpha1.PhaseAwaitingTestPlan
		if serr := r.Status().Update(ctx, preview); serr != nil {
			return ctrl.Result{}, nil, serr
		}
		return ctrl.Result{RequeueAfter: timeout / 6}, nil, nil

	case platformv1alpha1.TestPlanPhaseRejected:
		plan := r.applyFullSuiteFallback(ctx, preview, nsName, correlationID,
			platformv1alpha1.TestPlanSourceFallback)
		return ctrl.Result{}, plan, nil

	default: // Pending
		return r.handlePendingPlan(ctx, preview, nsName, existing, correlationID)
	}
}

// handlePendingPlan waits for the agent or triggers fallback on timeout.
func (r *PreviewReconciler) handlePendingPlan(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName string,
	plan *platformv1alpha1.TestPlan,
	correlationID string,
) (ctrl.Result, *platformv1alpha1.TestPlan, error) {
	logger := log.FromContext(ctx).WithValues("previewName", preview.Name)

	// If the agent filled the spec (generatedBy + confidence + mustRun), accept it
	// without requiring the agent to separately patch status.phase=Ready.
	if plan.Spec.GeneratedBy != "" && plan.Spec.Confidence > 0 && len(plan.Spec.MustRun) > 0 {
		logger.Info("agent filled TestPlan spec; promoting to Ready",
			"testPlan", plan.Name,
			"confidence", plan.Spec.Confidence,
		)
		now := metav1.Now()
		plan.Status.Phase = platformv1alpha1.TestPlanPhaseReady
		if err := r.Status().Update(ctx, plan); err != nil {
			return ctrl.Result{}, nil, err
		}
		plan.Status.AcceptedAt = &now
		return r.acceptOrFallback(ctx, preview, nsName, plan, correlationID)
	}

	timeoutSec := policy.AgentTimeoutSeconds(preview)
	deadline := plan.CreationTimestamp.Add(time.Duration(timeoutSec) * time.Second)

	if time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		logger.Info("TestPlan still pending, requeuing",
			"deadlineIn", remaining.Round(time.Second),
		)
		return ctrl.Result{RequeueAfter: remaining}, nil, nil
	}

	// Timeout reached — apply fallback policy.
	fallback := platformv1alpha1.TestStrategyFallbackFull
	if preview.Spec.TestStrategy != nil && preview.Spec.TestStrategy.FallbackOnAgentTimeout != "" {
		fallback = preview.Spec.TestStrategy.FallbackOnAgentTimeout
	}

	logger.Info("agent timeout reached",
		"fallback", fallback,
		"correlationID", correlationID,
	)

	switch fallback {
	case platformv1alpha1.TestStrategyFallbackSkip:
		r.updateTestPlanResolution(preview, platformv1alpha1.TestPlanSourceFallback, correlationID,
			"Agent timed out; tests skipped by fallbackOnAgentTimeout=Skip.")
		return ctrl.Result{}, nil, nil

	case platformv1alpha1.TestStrategyFallbackError:
		return ctrl.Result{}, nil, fmt.Errorf("agent timeout: no TestPlan available for preview %s", preview.Name)

	default: // Full
		result := r.applyFullSuiteFallback(ctx, preview, nsName, correlationID,
			platformv1alpha1.TestPlanSourceFallback)
		return ctrl.Result{}, result, nil
	}
}

// acceptOrFallback validates a Ready TestPlan and accepts or falls back.
func (r *PreviewReconciler) acceptOrFallback(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName string,
	plan *platformv1alpha1.TestPlan,
	correlationID string,
) (ctrl.Result, *platformv1alpha1.TestPlan, error) {
	logger := log.FromContext(ctx).WithValues("previewName", preview.Name, "testPlan", plan.Name)

	valid, reason := policy.ValidatePlan(plan)
	if !valid {
		logger.Info("TestPlan rejected: invalid structure", "reason", reason)
		plan.Status.Phase = platformv1alpha1.TestPlanPhaseRejected
		plan.Status.RejectionReason = reason
		plan.Status.AcceptedByController = false
		if err := r.Status().Update(ctx, plan); err != nil {
			return ctrl.Result{}, nil, err
		}
		r.emitReconcileEvent(ctx, preview, nsName, platformv1alpha1.ReconcileEventError,
			"", "Rejected", reason, correlationID)
		fallbackPlan := r.applyFullSuiteFallback(ctx, preview, nsName, correlationID,
			platformv1alpha1.TestPlanSourceFallback)
		return ctrl.Result{}, fallbackPlan, nil
	}

	threshold := policy.ConfidenceThreshold(preview)
	if !policy.IsConfident(plan, threshold) {
		reason := fmt.Sprintf("confidence %d < threshold %d", plan.Spec.Confidence, threshold)
		logger.Info("TestPlan confidence below threshold, falling back",
			"confidence", plan.Spec.Confidence,
			"threshold", threshold,
		)
		plan.Status.Phase = platformv1alpha1.TestPlanPhaseRejected
		plan.Status.RejectionReason = reason
		plan.Status.AcceptedByController = false
		if err := r.Status().Update(ctx, plan); err != nil {
			return ctrl.Result{}, nil, err
		}
		fallbackPlan := r.applyFullSuiteFallback(ctx, preview, nsName, correlationID,
			platformv1alpha1.TestPlanSourceFallback)
		return ctrl.Result{}, fallbackPlan, nil
	}

	now := metav1.Now()
	plan.Status.AcceptedByController = true
	plan.Status.AcceptedAt = &now
	if err := r.Status().Update(ctx, plan); err != nil {
		return ctrl.Result{}, nil, err
	}

	r.updateTestPlanResolution(preview, platformv1alpha1.TestPlanSourceAgent, correlationID,
		plan.Spec.Rationale)
	preview.Status.TestPlanRef = &corev1.ObjectReference{
		APIVersion: "platform.company.io/v1alpha1",
		Kind:       "TestPlan",
		Name:       plan.Name,
		Namespace:  plan.Namespace,
	}

	logger.Info("TestPlan accepted",
		"confidence", plan.Spec.Confidence,
		"decisionSource", "agent",
		"correlationID", correlationID,
	)
	return ctrl.Result{}, plan, nil
}

// resolveManualPlan handles mode=Manual.
func (r *PreviewReconciler) resolveManualPlan(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName, correlationID string,
) (*platformv1alpha1.TestPlan, error) {
	if preview.Spec.TestStrategy == nil || preview.Spec.TestStrategy.ManualPlanRef == nil {
		return r.applyFullSuiteFallback(ctx, preview, nsName, correlationID,
			platformv1alpha1.TestPlanSourceManual), nil
	}

	ref := preview.Spec.TestStrategy.ManualPlanRef
	plan := &platformv1alpha1.TestPlan{}
	key := client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}
	if err := r.Get(ctx, key, plan); err != nil {
		if errors.IsNotFound(err) {
			return r.applyFullSuiteFallback(ctx, preview, nsName, correlationID,
				platformv1alpha1.TestPlanSourceFallback), nil
		}
		return nil, err
	}
	if plan.Status.Phase == platformv1alpha1.TestPlanPhaseStale {
		return r.applyFullSuiteFallback(ctx, preview, nsName, correlationID,
			platformv1alpha1.TestPlanSourceFallback), nil
	}

	r.updateTestPlanResolution(preview, platformv1alpha1.TestPlanSourceManual, correlationID,
		plan.Spec.Rationale)
	preview.Status.TestPlanRef = &corev1.ObjectReference{
		APIVersion: "platform.company.io/v1alpha1",
		Kind:       "TestPlan",
		Name:       plan.Name,
		Namespace:  plan.Namespace,
	}
	return plan, nil
}

// applyFullSuiteFallback creates (or fetches) a FullSuite TestPlan and updates Preview status.
// Uses a deterministic name to stay idempotent across reconcile cycles.
func (r *PreviewReconciler) applyFullSuiteFallback(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName, correlationID string,
	source platformv1alpha1.TestPlanResolutionSource,
) *platformv1alpha1.TestPlan {
	logger := log.FromContext(ctx).WithValues("previewName", preview.Name)

	// Check if the FullSuite plan already exists and is accepted.
	existing := &platformv1alpha1.TestPlan{}
	planName := policy.FullSuitePlanName(preview.Name)
	if err := r.Get(ctx, client.ObjectKey{Name: planName, Namespace: nsName}, existing); err == nil {
		if existing.Status.Phase == platformv1alpha1.TestPlanPhaseReady && existing.Status.AcceptedByController {
			r.updateTestPlanResolution(preview, source, correlationID, existing.Spec.Rationale)
			preview.Status.TestPlanRef = &corev1.ObjectReference{
				APIVersion: "platform.company.io/v1alpha1",
				Kind:       "TestPlan",
				Name:       existing.Name,
				Namespace:  existing.Namespace,
			}
			return existing
		}
	}

	plan := policy.FullSuitePlan(preview, nsName, correlationID)
	if err := r.Create(ctx, plan); err != nil {
		if errors.IsAlreadyExists(err) {
			// Another reconcile already created it; fetch and use it.
			if ferr := r.Get(ctx, client.ObjectKey{Name: planName, Namespace: nsName}, plan); ferr != nil {
				logger.Error(ferr, "failed to fetch existing FullSuite TestPlan")
			}
		} else {
			logger.Error(err, "failed to persist FullSuite TestPlan")
		}
	}

	now := metav1.Now()
	plan.Status.Phase = platformv1alpha1.TestPlanPhaseReady
	plan.Status.AcceptedByController = true
	plan.Status.AcceptedAt = &now
	if serr := r.Status().Update(ctx, plan); serr != nil {
		logger.Error(serr, "failed to set FullSuite TestPlan status")
	}

	r.updateTestPlanResolution(preview, source, correlationID, plan.Spec.Rationale)
	preview.Status.TestPlanRef = &corev1.ObjectReference{
		APIVersion: "platform.company.io/v1alpha1",
		Kind:       "TestPlan",
		Name:       plan.Name,
		Namespace:  plan.Namespace,
	}

	logger.Info("resolved test plan",
		"decisionSource", source,
		"confidence", 100,
		"correlationID", correlationID,
	)
	return plan
}

// findActiveTestPlan returns the highest-priority non-stale TestPlan for this Preview.
func (r *PreviewReconciler) findActiveTestPlan(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName string,
) (*platformv1alpha1.TestPlan, error) {
	list := &platformv1alpha1.TestPlanList{}
	if err := r.List(ctx, list,
		client.InNamespace(nsName),
		client.MatchingLabels{policy.LabelPreviewName: preview.Name},
	); err != nil {
		return nil, err
	}
	if len(list.Items) == 0 {
		return nil, nil
	}

	currentSHA := ""
	if preview.Spec.ChangeContext != nil {
		currentSHA = preview.Spec.ChangeContext.DiffRef.HeadSHA
	}

	var best *platformv1alpha1.TestPlan
	for i := range list.Items {
		p := &list.Items[i]
		if currentSHA != "" && p.Spec.CommitSHA != "" && p.Spec.CommitSHA != currentSHA {
			if p.Status.Phase != platformv1alpha1.TestPlanPhaseStale {
				p.Status.Phase = platformv1alpha1.TestPlanPhaseStale
				_ = r.Status().Update(ctx, p)
			}
			continue
		}
		if p.Status.Phase == platformv1alpha1.TestPlanPhaseStale {
			continue
		}
		if best == nil || planPriority(p.Status.Phase) > planPriority(best.Status.Phase) {
			best = p
		}
	}
	return best, nil
}

func planPriority(phase platformv1alpha1.TestPlanPhase) int {
	switch phase {
	case platformv1alpha1.TestPlanPhaseReady:
		return 3
	case platformv1alpha1.TestPlanPhasePending:
		return 2
	case platformv1alpha1.TestPlanPhaseRejected:
		return 1
	default:
		return 0
	}
}

// createStubTestPlanAndReturn posts the empty TestPlan stub and returns it so the caller
// can pass the name to triggerTestStrategistAgent.
func (r *PreviewReconciler) createStubTestPlanAndReturn(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName, correlationID string,
) (*platformv1alpha1.TestPlan, error) {
	stub := policy.StubPlan(preview, nsName, correlationID)
	if err := r.Create(ctx, stub); err != nil {
		return nil, err
	}
	stub.Status.Phase = platformv1alpha1.TestPlanPhasePending
	if err := r.Status().Update(ctx, stub); err != nil {
		return nil, err
	}
	return stub, nil
}

// createStubTestPlan is kept for callers that don't need the returned plan.
func (r *PreviewReconciler) createStubTestPlan(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName, correlationID string,
) error {
	_, err := r.createStubTestPlanAndReturn(ctx, preview, nsName, correlationID)
	return err
}

// createTestRun persists a TestRun that records the planned test execution.
func (r *PreviewReconciler) createTestRun(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName string,
	plan *platformv1alpha1.TestPlan,
) error {
	if plan == nil {
		return nil
	}
	selected := append(append([]platformv1alpha1.TestSelector{}, plan.Spec.MustRun...), plan.Spec.ShouldRun...)
	now := metav1.Now()

	run := &platformv1alpha1.TestRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: preview.Name + "-run-",
			Namespace:    nsName,
			Labels: map[string]string{
				"platform.company.io/preview-name": preview.Name,
			},
		},
		Spec: platformv1alpha1.TestRunSpec{
			PreviewRef: corev1.ObjectReference{
				APIVersion: "platform.company.io/v1alpha1",
				Kind:       "Preview",
				Name:       preview.Name,
			},
			TestPlanRef: &corev1.ObjectReference{
				APIVersion: "platform.company.io/v1alpha1",
				Kind:       "TestPlan",
				Name:       plan.Name,
				Namespace:  plan.Namespace,
			},
			SelectedTests: selected,
			StartedAt:     &now,
			CorrelationID: plan.Spec.CorrelationID,
		},
	}

	if err := r.Create(ctx, run); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	run.Status.Phase = platformv1alpha1.TestRunPhaseRunning
	return r.Status().Update(ctx, run)
}

// updateTestPlanResolution records the resolution decision in Preview status.
func (r *PreviewReconciler) updateTestPlanResolution(
	preview *platformv1alpha1.Preview,
	source platformv1alpha1.TestPlanResolutionSource,
	correlationID, rationale string,
) {
	now := metav1.Now()
	preview.Status.TestPlanResolution = &platformv1alpha1.TestPlanResolutionStatus{
		Source:     source,
		ResolvedAt: &now,
		Rationale:  rationale,
	}
}

// makeCorrelationID returns a unique ID for this reconcile cycle.
func makeCorrelationID(preview *platformv1alpha1.Preview) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err == nil {
		return fmt.Sprintf("%s-%s", preview.Name, hex.EncodeToString(b))
	}
	return preview.Name
}
