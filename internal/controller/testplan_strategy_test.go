package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/policy"
)

func makeTestPreview(name string, mode platformv1alpha1.TestStrategyMode) *platformv1alpha1.Preview {
	p := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: platformv1alpha1.PreviewSpec{
			Branch:   "feat/test",
			PRNumber: 1,
			Image:    "nginx:alpine",
			TTL:      "24h",
			TestStrategy: &platformv1alpha1.TestStrategySpec{
				Mode:                   mode,
				AgentTimeoutSeconds:    30,
				ConfidenceThreshold:    70,
				FallbackOnAgentTimeout: platformv1alpha1.TestStrategyFallbackFull,
			},
		},
	}
	return p
}

func makeTestPreviewWithSHA(name string, mode platformv1alpha1.TestStrategyMode, sha string) *platformv1alpha1.Preview {
	p := makeTestPreview(name, mode)
	p.Spec.ChangeContext = &platformv1alpha1.ChangeContextSpec{
		DiffRef: platformv1alpha1.DiffRef{HeadSHA: sha},
	}
	return p
}

// TestReconcileTestStrategy_FullSuite verifies that mode=FullSuite always produces
// a FullSuite plan without creating a stub or interacting with the agent.
func TestReconcileTestStrategy_FullSuite(t *testing.T) {
	scheme := testAIScheme(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preview-pr-1"}}
	preview := makeTestPreview("pr-1", platformv1alpha1.TestStrategyFullSuite)

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(preview, &platformv1alpha1.TestPlan{}).
		WithObjects(preview, ns).
		Build()
	r := &PreviewReconciler{Client: cl, Scheme: scheme}

	result, plan, err := r.reconcileTestStrategy(context.Background(), preview, "preview-pr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter > 0 || result.Requeue {
		t.Fatalf("expected empty result, got %+v", result)
	}
	if plan == nil {
		t.Fatal("expected FullSuite plan, got nil")
	}
	if plan.Spec.GeneratedBy != platformv1alpha1.TestPlanByFallbackPolicy {
		t.Errorf("generatedBy = %q, want FallbackPolicy", plan.Spec.GeneratedBy)
	}
	if preview.Status.TestPlanResolution == nil {
		t.Fatal("expected testPlanResolution set in status")
	}
	if preview.Status.TestPlanResolution.Source != platformv1alpha1.TestPlanSourceFull {
		t.Errorf("resolution source = %q, want Full", preview.Status.TestPlanResolution.Source)
	}
}

// TestReconcileTestStrategy_Auto_NoExistingPlan verifies that mode=Auto with no
// existing TestPlan creates a stub and returns a RequeueAfter.
func TestReconcileTestStrategy_Auto_NoExistingPlan(t *testing.T) {
	scheme := testAIScheme(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preview-pr-2"}}
	preview := makeTestPreview("pr-2", platformv1alpha1.TestStrategyAuto)

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(preview, &platformv1alpha1.TestPlan{}).
		WithObjects(preview, ns).
		Build()
	r := &PreviewReconciler{Client: cl, Scheme: scheme}

	result, plan, err := r.reconcileTestStrategy(context.Background(), preview, "preview-pr-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan != nil {
		t.Fatalf("expected nil plan (awaiting agent), got %v", plan.Name)
	}
	if result.RequeueAfter == 0 && !result.Requeue {
		t.Error("expected requeue after creating stub, got empty result")
	}

	// Verify stub was created.
	list := &platformv1alpha1.TestPlanList{}
	if err := cl.List(context.Background(), list); err != nil {
		t.Fatalf("list TestPlans: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 stub TestPlan, got %d", len(list.Items))
	}
	if list.Items[0].Status.Phase != platformv1alpha1.TestPlanPhasePending {
		t.Errorf("stub phase = %q, want Pending", list.Items[0].Status.Phase)
	}
	if preview.Status.Phase != platformv1alpha1.PhaseAwaitingTestPlan {
		t.Errorf("preview phase = %q, want AwaitingTestPlan", preview.Status.Phase)
	}
}

// TestReconcileTestStrategy_Auto_HighConfidencePlan verifies that a Pending TestPlan
// filled by the agent (confidence >= threshold) is accepted without timeout.
func TestReconcileTestStrategy_Auto_HighConfidencePlan(t *testing.T) {
	scheme := testAIScheme(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preview-pr-3"}}
	preview := makeTestPreview("pr-3", platformv1alpha1.TestStrategyAuto)

	// Simulate agent filling the stub plan.
	stub := &platformv1alpha1.TestPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pr-3-abc123",
			Namespace: "preview-pr-3",
			Labels:    map[string]string{policy.LabelPreviewName: "pr-3"},
		},
		Spec: platformv1alpha1.TestPlanSpec{
			PreviewRef:  corev1.ObjectReference{Name: "pr-3"},
			GeneratedBy: platformv1alpha1.TestPlanByAgent,
			Confidence:  85,
			Rationale:   "only CSS changed",
			MustRun: []platformv1alpha1.TestSelector{
				{Suite: platformv1alpha1.TestSuiteSmoke, Name: "*", Reason: "baseline"},
			},
			CanSkip: []platformv1alpha1.TestSelector{
				{Suite: platformv1alpha1.TestSuiteContract, Name: "*", Reason: "openapi unchanged"},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(preview, stub).
		WithObjects(preview, ns, stub).
		Build()

	// Set phase=Pending on stub (status subresource).
	stub.Status.Phase = platformv1alpha1.TestPlanPhasePending
	if err := cl.Status().Update(context.Background(), stub); err != nil {
		t.Fatalf("set stub Pending: %v", err)
	}

	r := &PreviewReconciler{Client: cl, Scheme: scheme}
	result, plan, err := r.reconcileTestStrategy(context.Background(), preview, "preview-pr-3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter > 0 {
		t.Errorf("expected empty result (plan accepted), got RequeueAfter=%v", result.RequeueAfter)
	}
	if plan == nil {
		t.Fatal("expected accepted plan, got nil")
	}
	if plan.Spec.GeneratedBy != platformv1alpha1.TestPlanByAgent {
		t.Errorf("generatedBy = %q, want Agent", plan.Spec.GeneratedBy)
	}

	// Verify plan was promoted to Ready and accepted.
	var updated platformv1alpha1.TestPlan
	if err := cl.Get(context.Background(), types.NamespacedName{Name: stub.Name, Namespace: stub.Namespace}, &updated); err != nil {
		t.Fatalf("fetch updated plan: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.TestPlanPhaseReady {
		t.Errorf("plan phase = %q, want Ready", updated.Status.Phase)
	}
	if !updated.Status.AcceptedByController {
		t.Error("expected AcceptedByController=true")
	}
}

// TestReconcileTestStrategy_Auto_LowConfidencePlan verifies that a plan with
// confidence below the threshold is rejected and FullSuite fallback is applied.
func TestReconcileTestStrategy_Auto_LowConfidencePlan(t *testing.T) {
	scheme := testAIScheme(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preview-pr-4"}}
	preview := makeTestPreview("pr-4", platformv1alpha1.TestStrategyAuto)

	stub := &platformv1alpha1.TestPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pr-4-lowconf",
			Namespace: "preview-pr-4",
			Labels:    map[string]string{policy.LabelPreviewName: "pr-4"},
		},
		Spec: platformv1alpha1.TestPlanSpec{
			PreviewRef:  corev1.ObjectReference{Name: "pr-4"},
			GeneratedBy: platformv1alpha1.TestPlanByAgent,
			Confidence:  55, // below threshold of 70
			Rationale:   "uncertain",
			MustRun: []platformv1alpha1.TestSelector{
				{Suite: platformv1alpha1.TestSuiteSmoke, Name: "*", Reason: "baseline"},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(preview, stub).
		WithObjects(preview, ns, stub).
		Build()
	stub.Status.Phase = platformv1alpha1.TestPlanPhasePending
	if err := cl.Status().Update(context.Background(), stub); err != nil {
		t.Fatalf("set stub Pending: %v", err)
	}

	r := &PreviewReconciler{Client: cl, Scheme: scheme}
	_, plan, err := r.reconcileTestStrategy(context.Background(), preview, "preview-pr-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil {
		t.Fatal("expected fallback plan, got nil")
	}
	if plan.Spec.GeneratedBy != platformv1alpha1.TestPlanByFallbackPolicy {
		t.Errorf("expected fallback plan, got generatedBy=%q", plan.Spec.GeneratedBy)
	}

	// Original stub should be marked Rejected.
	var updated platformv1alpha1.TestPlan
	if err := cl.Get(context.Background(), types.NamespacedName{Name: stub.Name, Namespace: stub.Namespace}, &updated); err != nil {
		t.Fatalf("fetch stub: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.TestPlanPhaseRejected {
		t.Errorf("stub phase = %q, want Rejected", updated.Status.Phase)
	}
}

// TestReconcileTestStrategy_Auto_Timeout verifies that a Pending TestPlan that
// has not been filled within agentTimeoutSeconds triggers the fallback policy.
func TestReconcileTestStrategy_Auto_Timeout(t *testing.T) {
	scheme := testAIScheme(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preview-pr-5"}}
	preview := makeTestPreview("pr-5", platformv1alpha1.TestStrategyAuto)

	// Stub created 2 minutes ago — well past the 30s timeout.
	oldTime := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	stub := &platformv1alpha1.TestPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pr-5-timedout",
			Namespace:         "preview-pr-5",
			Labels:            map[string]string{policy.LabelPreviewName: "pr-5"},
			CreationTimestamp: oldTime,
		},
		Spec: platformv1alpha1.TestPlanSpec{
			PreviewRef: corev1.ObjectReference{Name: "pr-5"},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(preview, stub).
		WithObjects(preview, ns, stub).
		Build()
	stub.Status.Phase = platformv1alpha1.TestPlanPhasePending
	if err := cl.Status().Update(context.Background(), stub); err != nil {
		t.Fatalf("set stub Pending: %v", err)
	}

	r := &PreviewReconciler{Client: cl, Scheme: scheme}
	result, plan, err := r.reconcileTestStrategy(context.Background(), preview, "preview-pr-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter > 0 {
		t.Errorf("expected empty result after timeout, got RequeueAfter=%v", result.RequeueAfter)
	}
	if plan == nil {
		t.Fatal("expected fallback plan after timeout, got nil")
	}
	if plan.Spec.GeneratedBy != platformv1alpha1.TestPlanByFallbackPolicy {
		t.Errorf("expected fallback plan, got generatedBy=%q", plan.Spec.GeneratedBy)
	}
	if preview.Status.TestPlanResolution == nil {
		t.Fatal("expected testPlanResolution set")
	}
	if preview.Status.TestPlanResolution.Source != platformv1alpha1.TestPlanSourceFallback {
		t.Errorf("resolution source = %q, want Fallback", preview.Status.TestPlanResolution.Source)
	}
}

// TestReconcileTestStrategy_Auto_CommitSHAChange verifies that a TestPlan for
// an old commitSHA is marked Stale when the Preview's SHA changes.
func TestReconcileTestStrategy_Auto_CommitSHAChange(t *testing.T) {
	scheme := testAIScheme(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preview-pr-6"}}
	preview := makeTestPreviewWithSHA("pr-6", platformv1alpha1.TestStrategyAuto, "newsha456")

	// Accepted plan for the OLD sha.
	oldPlan := &platformv1alpha1.TestPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pr-6-oldplan",
			Namespace: "preview-pr-6",
			Labels: map[string]string{
				policy.LabelPreviewName: "pr-6",
				policy.LabelCommitSHA:   "oldsha123",
			},
		},
		Spec: platformv1alpha1.TestPlanSpec{
			PreviewRef:  corev1.ObjectReference{Name: "pr-6"},
			GeneratedBy: platformv1alpha1.TestPlanByAgent,
			Confidence:  90,
			CommitSHA:   "oldsha123",
			MustRun: []platformv1alpha1.TestSelector{
				{Suite: platformv1alpha1.TestSuiteSmoke, Name: "*", Reason: "baseline"},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(preview, oldPlan).
		WithObjects(preview, ns, oldPlan).
		Build()
	oldPlan.Status.Phase = platformv1alpha1.TestPlanPhaseReady
	oldPlan.Status.AcceptedByController = true
	if err := cl.Status().Update(context.Background(), oldPlan); err != nil {
		t.Fatalf("set oldPlan Ready: %v", err)
	}

	r := &PreviewReconciler{Client: cl, Scheme: scheme}
	result, plan, err := r.reconcileTestStrategy(context.Background(), preview, "preview-pr-6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Old plan should now be Stale; controller creates a new stub → RequeueAfter.
	var updatedOld platformv1alpha1.TestPlan
	if err := cl.Get(context.Background(), types.NamespacedName{Name: oldPlan.Name, Namespace: oldPlan.Namespace}, &updatedOld); err != nil {
		t.Fatalf("fetch old plan: %v", err)
	}
	if updatedOld.Status.Phase != platformv1alpha1.TestPlanPhaseStale {
		t.Errorf("old plan phase = %q, want Stale", updatedOld.Status.Phase)
	}
	if plan != nil {
		t.Errorf("expected nil plan (awaiting new agent plan), got %v", plan.Name)
	}
	if result.RequeueAfter == 0 && !result.Requeue {
		t.Error("expected requeue after creating new stub")
	}
}

// TestReconcileTestStrategy_Auto_MalformedPlan verifies that a TestPlan where
// the same suite appears in both mustRun and canSkip is rejected.
func TestReconcileTestStrategy_Auto_MalformedPlan(t *testing.T) {
	scheme := testAIScheme(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preview-pr-7"}}
	preview := makeTestPreview("pr-7", platformv1alpha1.TestStrategyAuto)

	// Agent produced a malformed plan: smoke in both mustRun and canSkip.
	badPlan := &platformv1alpha1.TestPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pr-7-bad",
			Namespace: "preview-pr-7",
			Labels:    map[string]string{policy.LabelPreviewName: "pr-7"},
		},
		Spec: platformv1alpha1.TestPlanSpec{
			PreviewRef:  corev1.ObjectReference{Name: "pr-7"},
			GeneratedBy: platformv1alpha1.TestPlanByAgent,
			Confidence:  85,
			MustRun: []platformv1alpha1.TestSelector{
				{Suite: platformv1alpha1.TestSuiteSmoke, Name: "*", Reason: "baseline"},
			},
			CanSkip: []platformv1alpha1.TestSelector{
				{Suite: platformv1alpha1.TestSuiteSmoke, Name: "*", Reason: "bad agent hallucination"},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(preview, badPlan).
		WithObjects(preview, ns, badPlan).
		Build()
	badPlan.Status.Phase = platformv1alpha1.TestPlanPhasePending
	if err := cl.Status().Update(context.Background(), badPlan); err != nil {
		t.Fatalf("set plan Pending: %v", err)
	}

	r := &PreviewReconciler{Client: cl, Scheme: scheme}
	_, plan, err := r.reconcileTestStrategy(context.Background(), preview, "preview-pr-7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil {
		t.Fatal("expected fallback plan after rejection, got nil")
	}
	if plan.Spec.GeneratedBy != platformv1alpha1.TestPlanByFallbackPolicy {
		t.Errorf("expected fallback plan, got generatedBy=%q", plan.Spec.GeneratedBy)
	}

	// Bad plan should be Rejected.
	var updated platformv1alpha1.TestPlan
	if err := cl.Get(context.Background(), types.NamespacedName{Name: badPlan.Name, Namespace: badPlan.Namespace}, &updated); err != nil {
		t.Fatalf("fetch bad plan: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.TestPlanPhaseRejected {
		t.Errorf("bad plan phase = %q, want Rejected", updated.Status.Phase)
	}
	if updated.Status.RejectionReason == "" {
		t.Error("expected rejection reason to be set")
	}
}
