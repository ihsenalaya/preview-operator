package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReconcileEventType classifies a controller lifecycle event.
// +kubebuilder:validation:Enum=Provisioned;TestStarted;TestFinished;Error;Ready
type ReconcileEventType string

const (
	ReconcileEventProvisioned   ReconcileEventType = "Provisioned"
	ReconcileEventTestStarted   ReconcileEventType = "TestStarted"
	ReconcileEventTestFinished  ReconcileEventType = "TestFinished"
	ReconcileEventError         ReconcileEventType = "Error"
	ReconcileEventReady         ReconcileEventType = "Ready"
)

// ReconcileEventSpec defines the content of one event.
type ReconcileEventSpec struct {
	// PreviewRef references the Preview this event belongs to.
	// +kubebuilder:validation:Required
	PreviewRef corev1.ObjectReference `json:"previewRef"`

	// Type classifies the event.
	// +kubebuilder:validation:Required
	Type ReconcileEventType `json:"type"`

	// TestSuite identifies the test suite (when Type is TestStarted or TestFinished).
	// +optional
	TestSuite string `json:"testSuite,omitempty"`

	// Outcome is a brief result string (e.g. "Succeeded", "Failed:exit-1").
	// +optional
	Outcome string `json:"outcome,omitempty"`

	// Message is a human-readable description of the event.
	// +optional
	Message string `json:"message,omitempty"`

	// FilePatterns is a denormalized subset of changed file paths from the
	// associated ChangeContext, included here so agents can query ReconcileEvents
	// by file pattern without joining back to the Preview.
	// +optional
	FilePatterns []string `json:"filePatterns,omitempty"`

	// OccurredAt is when the event happened.
	// +optional
	OccurredAt *metav1.Time `json:"occurredAt,omitempty"`

	// CorrelationID ties this event to the reconcile cycle that produced it.
	// +optional
	CorrelationID string `json:"correlationID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=re
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Suite",type=string,JSONPath=`.spec.testSuite`
// +kubebuilder:printcolumn:name="Outcome",type=string,JSONPath=`.spec.outcome`
// +kubebuilder:printcolumn:name="OccurredAt",type=string,JSONPath=`.spec.occurredAt`

// ReconcileEvent is an append-only record written by the controller each time
// something material happens to a Preview. The test-strategist agent reads
// recent ReconcileEvents as historical signal when deciding which tests to run.
type ReconcileEvent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ReconcileEventSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ReconcileEventList contains a list of ReconcileEvent.
type ReconcileEventList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ReconcileEvent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ReconcileEvent{}, &ReconcileEventList{})
}
