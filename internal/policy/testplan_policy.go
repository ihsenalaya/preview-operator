// Package policy contains the test-strategy decision logic for the Preview controller.
// The controller uses these helpers to decide whether to accept, reject, or fall back
// when evaluating a TestPlan produced by the kagent test-strategist agent.
package policy

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

const (
	// AnnotationRequestedBy is placed on agent-requested TestPlans so
	// the controller and debugging tools can trace ownership.
	AnnotationRequestedBy = "idp-preview.io/requested-by"

	// LabelPreviewName allows efficient list queries.
	LabelPreviewName = "platform.company.io/preview-name"

	// LabelCommitSHA links a TestPlan to the commit it was generated for.
	LabelCommitSHA = "platform.company.io/commit-sha"
)

// ValidatePlan checks the hard invariants on a TestPlan that the agent must satisfy.
// Returns false and a reason string if the plan is invalid.
func ValidatePlan(plan *platformv1alpha1.TestPlan) (bool, string) {
	mustRunSet := make(map[string]struct{}, len(plan.Spec.MustRun))
	for _, s := range plan.Spec.MustRun {
		mustRunSet[selectorKey(s)] = struct{}{}
	}
	for _, s := range plan.Spec.CanSkip {
		if _, conflict := mustRunSet[selectorKey(s)]; conflict {
			return false, fmt.Sprintf("test %q appears in both mustRun and canSkip", selectorKey(s))
		}
	}
	return true, ""
}

// IsConfident returns true when the plan's confidence meets or exceeds the threshold.
func IsConfident(plan *platformv1alpha1.TestPlan, threshold int) bool {
	return plan.Spec.Confidence >= threshold
}

// FullSuitePlanName returns the deterministic name for the FullSuite TestPlan of a Preview.
// A stable name lets the controller use IsAlreadyExists to stay idempotent.
func FullSuitePlanName(previewName string) string {
	return previewName + "-fullsuite"
}

// FullSuitePlan builds an internal TestPlan that selects all standard suites.
// Used when mode=FullSuite or when the agent falls back.
func FullSuitePlan(preview *platformv1alpha1.Preview, nsName, correlationID string) *platformv1alpha1.TestPlan {
	allSuites := []platformv1alpha1.TestSuite{
		platformv1alpha1.TestSuiteSmoke,
		platformv1alpha1.TestSuiteContract,
		platformv1alpha1.TestSuiteRegression,
		platformv1alpha1.TestSuiteE2E,
	}
	mustRun := make([]platformv1alpha1.TestSelector, 0, len(allSuites))
	for _, s := range allSuites {
		mustRun = append(mustRun, platformv1alpha1.TestSelector{
			Suite:  s,
			Name:   "*",
			Reason: "FullSuite mode",
		})
	}

	now := metav1.Now()
	commitSHA := commitSHAFrom(preview)

	plan := &platformv1alpha1.TestPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      FullSuitePlanName(preview.Name),
			Namespace: nsName,
			Labels: map[string]string{
				LabelPreviewName: preview.Name,
				LabelCommitSHA:   commitSHA,
			},
			Annotations: map[string]string{
				AnnotationRequestedBy: preview.Name,
			},
		},
		Spec: platformv1alpha1.TestPlanSpec{
			PreviewRef: corev1.ObjectReference{
				APIVersion: "platform.company.io/v1alpha1",
				Kind:       "Preview",
				Name:       preview.Name,
			},
			GeneratedBy:   platformv1alpha1.TestPlanByFallbackPolicy,
			Confidence:    100,
			MustRun:       mustRun,
			Rationale:     "Full suite selected by FallbackPolicy (no agent plan available or mode=FullSuite).",
			GeneratedAt:   &now,
			CommitSHA:     commitSHA,
			CorrelationID: correlationID,
		},
	}
	return plan
}

// StubPlan creates the empty TestPlan stub that the controller posts for the agent to fill.
// The agent watches for TestPlans in phase Pending and fills them in.
func StubPlan(preview *platformv1alpha1.Preview, nsName, correlationID string) *platformv1alpha1.TestPlan {
	commitSHA := commitSHAFrom(preview)
	return &platformv1alpha1.TestPlan{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: preview.Name + "-",
			Namespace:    nsName,
			Labels: map[string]string{
				LabelPreviewName: preview.Name,
				LabelCommitSHA:   commitSHA,
			},
			Annotations: map[string]string{
				AnnotationRequestedBy: preview.Name,
			},
		},
		Spec: platformv1alpha1.TestPlanSpec{
			PreviewRef: corev1.ObjectReference{
				APIVersion: "platform.company.io/v1alpha1",
				Kind:       "Preview",
				Name:       preview.Name,
			},
			CommitSHA:     commitSHA,
			CorrelationID: correlationID,
		},
	}
}

// ConfidenceThreshold returns the effective threshold for a Preview, defaulting to 70.
func ConfidenceThreshold(preview *platformv1alpha1.Preview) int {
	if preview.Spec.TestStrategy == nil {
		return 70
	}
	if preview.Spec.TestStrategy.ConfidenceThreshold == 0 {
		return 70
	}
	return preview.Spec.TestStrategy.ConfidenceThreshold
}

// AgentTimeoutSeconds returns the effective agent timeout for a Preview.
func AgentTimeoutSeconds(preview *platformv1alpha1.Preview) int {
	if preview.Spec.TestStrategy == nil {
		return 60
	}
	if preview.Spec.TestStrategy.AgentTimeoutSeconds == 0 {
		return 60
	}
	return preview.Spec.TestStrategy.AgentTimeoutSeconds
}

// LabelExperimentOwned, when set to "true" on a Preview, forces FullSuite mode
// regardless of the spec. The failure-provenance evaluation harness uses this
// label so the LLM test-plan strategist cannot non-deterministically drop the
// very suite that would exercise the injected fault (e.g. dropping `contract`
// for an injection that breaks a contract endpoint).
const LabelExperimentOwned = "failure-provenance.experiment/owned"

// EffectiveMode returns the test strategy mode, defaulting to FullSuite.
// Previews labelled with LabelExperimentOwned=true are always run in FullSuite
// — the failure-provenance evaluation requires deterministic suite coverage
// and the AI strategist's selection is intentionally bypassed for those runs.
func EffectiveMode(preview *platformv1alpha1.Preview) platformv1alpha1.TestStrategyMode {
	if preview.Labels[LabelExperimentOwned] == "true" {
		return platformv1alpha1.TestStrategyFullSuite
	}
	if preview.Spec.TestStrategy == nil || preview.Spec.TestStrategy.Mode == "" {
		return platformv1alpha1.TestStrategyFullSuite
	}
	return preview.Spec.TestStrategy.Mode
}

// IsSuiteSelected returns true when the given suite should run given an accepted TestPlan.
// A suite is selected if it appears in mustRun or shouldRun and does NOT appear in canSkip.
// When plan is nil (FullSuite fallback), all suites are selected.
func IsSuiteSelected(plan *platformv1alpha1.TestPlan, suite platformv1alpha1.TestSuite) bool {
	if plan == nil {
		return true
	}
	for _, s := range plan.Spec.CanSkip {
		if s.Suite == suite {
			return false
		}
	}
	for _, s := range plan.Spec.MustRun {
		if s.Suite == suite {
			return true
		}
	}
	for _, s := range plan.Spec.ShouldRun {
		if s.Suite == suite {
			return true
		}
	}
	// If plan has content but suite is not mentioned, skip it.
	if len(plan.Spec.MustRun)+len(plan.Spec.ShouldRun) > 0 {
		return false
	}
	return true
}

func selectorKey(s platformv1alpha1.TestSelector) string {
	return fmt.Sprintf("%s/%s", s.Suite, s.Name)
}

func commitSHAFrom(preview *platformv1alpha1.Preview) string {
	if preview.Spec.ChangeContext != nil {
		return preview.Spec.ChangeContext.DiffRef.HeadSHA
	}
	return ""
}
