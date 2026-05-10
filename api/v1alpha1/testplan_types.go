package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSuite identifies a named test suite.
// +kubebuilder:validation:Enum=smoke;contract;regression;migration;e2e;load
type TestSuite string

const (
	TestSuiteSmoke      TestSuite = "smoke"
	TestSuiteContract   TestSuite = "contract"
	TestSuiteRegression TestSuite = "regression"
	TestSuiteMigration  TestSuite = "migration"
	TestSuiteE2E        TestSuite = "e2e"
	TestSuiteLoad       TestSuite = "load"
)

// TestPlanGeneratedBy identifies who produced the TestPlan.
// +kubebuilder:validation:Enum=Agent;Manual;FallbackPolicy
type TestPlanGeneratedBy string

const (
	TestPlanByAgent          TestPlanGeneratedBy = "Agent"
	TestPlanByManual         TestPlanGeneratedBy = "Manual"
	TestPlanByFallbackPolicy TestPlanGeneratedBy = "FallbackPolicy"
)

// TestPlanPhase describes the TestPlan lifecycle.
// +kubebuilder:validation:Enum=Pending;Ready;Stale;Rejected
type TestPlanPhase string

const (
	TestPlanPhasePending  TestPlanPhase = "Pending"
	TestPlanPhaseReady    TestPlanPhase = "Ready"
	TestPlanPhaseStale    TestPlanPhase = "Stale"
	TestPlanPhaseRejected TestPlanPhase = "Rejected"
)

// TestSelector identifies a specific test or group of tests within a suite.
type TestSelector struct {
	// Suite is the test category.
	Suite TestSuite `json:"suite"`

	// Name is the test or scenario identifier. Use "*" to select all tests in a suite.
	// +kubebuilder:default="*"
	// +optional
	Name string `json:"name,omitempty"`

	// Reason is a human-readable explanation for why this test was selected or skipped.
	// +optional
	Reason string `json:"reason,omitempty"`
}

// TestPlanSpec defines the desired state of a TestPlan.
type TestPlanSpec struct {
	// PreviewRef references the Preview that requested this plan.
	// +kubebuilder:validation:Required
	PreviewRef corev1.ObjectReference `json:"previewRef"`

	// GeneratedBy identifies who produced this plan.
	// +optional
	GeneratedBy TestPlanGeneratedBy `json:"generatedBy,omitempty"`

	// AgentName is the kagent agent that produced this plan (when GeneratedBy=Agent).
	// +optional
	AgentName string `json:"agentName,omitempty"`

	// AgentVersion is the version of the agent that produced this plan.
	// +optional
	AgentVersion string `json:"agentVersion,omitempty"`

	// Confidence is the agent's self-reported confidence score (0-100).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	Confidence int `json:"confidence,omitempty"`

	// MustRun contains tests the agent considers mandatory for this diff.
	// +optional
	MustRun []TestSelector `json:"mustRun,omitempty"`

	// ShouldRun contains tests the agent recommends but does not consider mandatory.
	// +optional
	ShouldRun []TestSelector `json:"shouldRun,omitempty"`

	// CanSkip contains tests the agent considers safe to skip for this diff.
	// +optional
	CanSkip []TestSelector `json:"canSkip,omitempty"`

	// Rationale is a human-readable explanation of the test plan decision,
	// included verbatim in the PR comment.
	// +optional
	Rationale string `json:"rationale,omitempty"`

	// EstimatedDurationSeconds is the agent's estimate of total test time.
	// +optional
	EstimatedDurationSeconds int `json:"estimatedDurationSeconds,omitempty"`

	// GeneratedAt is when the agent produced this plan.
	// +optional
	GeneratedAt *metav1.Time `json:"generatedAt,omitempty"`

	// ExpiresAt is when this plan becomes invalid (typically when the commit SHA changes).
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// CommitSHA is the head commit this plan was produced for.
	// The controller marks the plan Stale when the Preview's commit SHA changes.
	// +optional
	CommitSHA string `json:"commitSHA,omitempty"`

	// CorrelationID is propagated across all CRs related to one reconcile cycle.
	// +optional
	CorrelationID string `json:"correlationID,omitempty"`
}

// TestPlanStatus describes the observed state of a TestPlan.
type TestPlanStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase TestPlanPhase `json:"phase,omitempty"`

	// AcceptedByController is true when the controller has validated and accepted this plan.
	// +optional
	AcceptedByController bool `json:"acceptedByController,omitempty"`

	// AcceptedAt is when the controller accepted this plan.
	// +optional
	AcceptedAt *metav1.Time `json:"acceptedAt,omitempty"`

	// RejectionReason explains why the controller rejected this plan.
	// +optional
	RejectionReason string `json:"rejectionReason,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=tp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Confidence",type=integer,JSONPath=`.spec.confidence`
// +kubebuilder:printcolumn:name="GeneratedBy",type=string,JSONPath=`.spec.generatedBy`
// +kubebuilder:printcolumn:name="Accepted",type=boolean,JSONPath=`.status.acceptedByController`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TestPlan is the output of the test-strategist agent. The agent writes it;
// the controller reads it and decides whether to accept or fall back to FullSuite.
type TestPlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TestPlanSpec   `json:"spec,omitempty"`
	Status TestPlanStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TestPlanList contains a list of TestPlan.
type TestPlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TestPlan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TestPlan{}, &TestPlanList{})
}
