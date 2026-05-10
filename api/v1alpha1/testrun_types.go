package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestRunPhase describes the TestRun lifecycle.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;PartiallyFailed
type TestRunPhase string

const (
	TestRunPhasePending         TestRunPhase = "Pending"
	TestRunPhaseRunning         TestRunPhase = "Running"
	TestRunPhaseSucceeded       TestRunPhase = "Succeeded"
	TestRunPhaseFailed          TestRunPhase = "Failed"
	TestRunPhasePartiallyFailed TestRunPhase = "PartiallyFailed"
)

// TestRunResult holds the outcome of one test execution within a TestRun.
type TestRunResult struct {
	// Suite is the test category.
	// +optional
	Suite TestSuite `json:"suite,omitempty"`

	// Name is the test identifier.
	// +optional
	Name string `json:"name,omitempty"`

	// Status is the outcome: Pending | Running | Succeeded | Failed | Skipped.
	// +optional
	Status string `json:"status,omitempty"`

	// DurationSeconds is how long the test took.
	// +optional
	DurationSeconds int `json:"durationSeconds,omitempty"`

	// LogsURL is a link to the raw test logs.
	// +optional
	LogsURL string `json:"logsURL,omitempty"`

	// JUnitURL is a link to the JUnit XML report.
	// +optional
	JUnitURL string `json:"junitURL,omitempty"`
}

// TestRunSpec defines the desired state of a TestRun.
type TestRunSpec struct {
	// PreviewRef references the Preview this run belongs to.
	// +kubebuilder:validation:Required
	PreviewRef corev1.ObjectReference `json:"previewRef"`

	// TestPlanRef references the TestPlan that was accepted for this run.
	// +optional
	TestPlanRef *corev1.ObjectReference `json:"testPlanRef,omitempty"`

	// SelectedTests is the final set of tests chosen after applying policy.
	// +optional
	SelectedTests []TestSelector `json:"selectedTests,omitempty"`

	// StartedAt is when the controller began launching test jobs.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CorrelationID is propagated from the accepted TestPlan.
	// +optional
	CorrelationID string `json:"correlationID,omitempty"`
}

// TestRunStatus describes the observed state of a TestRun.
type TestRunStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase TestRunPhase `json:"phase,omitempty"`

	// Results contains per-test outcomes appended as jobs complete.
	// +optional
	Results []TestRunResult `json:"results,omitempty"`

	// FinishedAt is when all test jobs completed.
	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=tr
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TestRun is created by the controller once a TestPlan is accepted. It tracks
// the execution of test jobs and persists results for historical analysis.
type TestRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TestRunSpec   `json:"spec,omitempty"`
	Status TestRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TestRunList contains a list of TestRun.
type TestRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TestRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TestRun{}, &TestRunList{})
}
