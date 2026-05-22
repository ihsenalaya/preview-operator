package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EvidenceType is the closed vocabulary of failure evidence item types.
// Each value maps to a W3C PROV Entity sub-type — see
// docs/research/failure-provenance/failure-evidence-model.md.
// +kubebuilder:validation:Enum=GitDiff;ChangedFile;KubernetesEvent;PodLog;JobLog;TestResult;TraceSpan;Metric;PreviewCondition;ReconcileEvent
type EvidenceType string

const (
	EvidenceTypeGitDiff          EvidenceType = "GitDiff"
	EvidenceTypeChangedFile      EvidenceType = "ChangedFile"
	EvidenceTypeKubernetesEvent  EvidenceType = "KubernetesEvent"
	EvidenceTypePodLog           EvidenceType = "PodLog"
	EvidenceTypeJobLog           EvidenceType = "JobLog"
	EvidenceTypeTestResult       EvidenceType = "TestResult"
	EvidenceTypeTraceSpan        EvidenceType = "TraceSpan"
	EvidenceTypeMetric           EvidenceType = "Metric"
	EvidenceTypePreviewCondition EvidenceType = "PreviewCondition"
	EvidenceTypeReconcileEvent   EvidenceType = "ReconcileEvent"
)

// EvidenceRelevance ranks how relevant an evidence item is to the probable cause.
// +kubebuilder:validation:Enum=high;medium;low
type EvidenceRelevance string

const (
	EvidenceRelevanceHigh   EvidenceRelevance = "high"
	EvidenceRelevanceMedium EvidenceRelevance = "medium"
	EvidenceRelevanceLow    EvidenceRelevance = "low"
)

// FailureCategory classifies the failure family of a diagnosis.
// +kubebuilder:validation:Enum=database;configuration;infrastructure;application;observability;test-reliability;unknown
type FailureCategory string

const (
	FailureCategoryDatabase        FailureCategory = "database"
	FailureCategoryConfiguration   FailureCategory = "configuration"
	FailureCategoryInfrastructure  FailureCategory = "infrastructure"
	FailureCategoryApplication     FailureCategory = "application"
	FailureCategoryObservability   FailureCategory = "observability"
	FailureCategoryTestReliability FailureCategory = "test-reliability"
	FailureCategoryUnknown         FailureCategory = "unknown"
)

// FailureEvidenceItem is one typed, addressable item of failure evidence.
// It corresponds to a W3C PROV Entity. The ID is stable and deterministic so that
// repeated reconciliation produces the same item and a diagnosis can reference it.
type FailureEvidenceItem struct {
	// ID is the stable, deterministic identifier of this evidence item.
	// It is derived from the item's logical identity (type, source, resource,
	// content) so repeated reconciliation yields the same ID.
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// Type classifies the evidence item.
	// +kubebuilder:validation:Required
	Type EvidenceType `json:"type"`

	// Source identifies what produced the evidence (an activity, tool, or collector).
	// +optional
	Source string `json:"source,omitempty"`

	// Resource identifies the Kubernetes object or file the evidence is about.
	// +optional
	Resource string `json:"resource,omitempty"`

	// Message is the (redacted) content of the evidence item.
	// +optional
	Message string `json:"message,omitempty"`

	// Timestamp is when the evidence was observed, when known.
	// +optional
	Timestamp *metav1.Time `json:"timestamp,omitempty"`

	// Relevance ranks the item against the probable cause.
	// +optional
	Relevance EvidenceRelevance `json:"relevance,omitempty"`

	// Redacted is true when secret-bearing content was redacted from Message.
	// +optional
	Redacted bool `json:"redacted,omitempty"`
}

// FailureDiagnosis is a probable cause grounded in collected evidence.
// Every entry in EvidenceRefs must reference the ID of an existing
// FailureEvidenceItem — this is the grounding constraint evaluated by RQ4.
type FailureDiagnosis struct {
	// ProbableCause is the human-readable probable root cause.
	// +optional
	ProbableCause string `json:"probableCause,omitempty"`

	// Component identifies the component most likely responsible.
	// +optional
	Component string `json:"component,omitempty"`

	// Category classifies the failure family.
	// +optional
	Category FailureCategory `json:"category,omitempty"`

	// Confidence estimates the strength of the diagnostic signal.
	// Kept as a string (low|medium|high) for consistency with the existing
	// DiagnosticsStatus.Confidence field used elsewhere in this operator.
	// +kubebuilder:validation:Enum=low;medium;high
	// +optional
	Confidence string `json:"confidence,omitempty"`

	// EvidenceRefs lists the FailureEvidenceItem IDs that support this diagnosis.
	// A diagnosis with no evidence reference is, by construction, ungrounded.
	// +optional
	EvidenceRefs []string `json:"evidenceRefs,omitempty"`

	// Recommendations lists suggested remediation actions.
	// +optional
	Recommendations []string `json:"recommendations,omitempty"`
}

// ProvenanceNodeKind classifies a provenance graph node onto the three W3C PROV
// core types.
// +kubebuilder:validation:Enum=entity;activity;agent
type ProvenanceNodeKind string

const (
	ProvenanceNodeEntity   ProvenanceNodeKind = "entity"
	ProvenanceNodeActivity ProvenanceNodeKind = "activity"
	ProvenanceNodeAgent    ProvenanceNodeKind = "agent"
)

// ProvenanceNode is one node of the failure provenance graph.
type ProvenanceNode struct {
	// ID uniquely identifies the node within the graph.
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// Kind is the W3C PROV core type of the node.
	// +kubebuilder:validation:Required
	Kind ProvenanceNodeKind `json:"kind"`

	// Type is the domain type of the node (e.g. PullRequest, Commit, ChangedFile,
	// Preview, Namespace, EvidenceCollection, Diagnosis, PreviewOperator).
	// +optional
	Type string `json:"type,omitempty"`

	// Label is a short human-readable label for the node.
	// +optional
	Label string `json:"label,omitempty"`
}

// ProvenanceEdge is one directed edge of the failure provenance graph. Relation is
// a W3C PROV relation (wasDerivedFrom, wasGeneratedBy, used, wasAssociatedWith) or
// a domain relation (supports, caused, observedIn).
type ProvenanceEdge struct {
	// From is the ID of the source node.
	// +kubebuilder:validation:Required
	From string `json:"from"`

	// To is the ID of the target node.
	// +kubebuilder:validation:Required
	To string `json:"to"`

	// Relation names the provenance relation.
	// +kubebuilder:validation:Required
	Relation string `json:"relation"`
}

// ProvenanceGraph links the change context, the preview resources, the captured
// evidence, and the diagnosis into one auditable graph, aligned with W3C PROV.
// It is the materialised form of the failure provenance graph (Contribution 2);
// see docs/research/failure-provenance/failure-evidence-model.md.
type ProvenanceGraph struct {
	// Nodes are the provenance graph nodes.
	// +optional
	Nodes []ProvenanceNode `json:"nodes,omitempty"`

	// Edges are the directed provenance relations between nodes.
	// +optional
	Edges []ProvenanceEdge `json:"edges,omitempty"`
}

// FailureReportSpec identifies the preview failure this report is about.
type FailureReportSpec struct {
	// PreviewRef references the Preview whose failure produced this report.
	// +kubebuilder:validation:Required
	PreviewRef corev1.ObjectReference `json:"previewRef"`

	// Namespace is the ephemeral preview namespace (which may already be deleted).
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// PRNumber is the pull request number. Kept as an int for consistency with
	// PreviewSpec.PRNumber and DiffRef.PullRequestNumber.
	// +optional
	PRNumber int `json:"prNumber,omitempty"`

	// CommitSHA is the head commit SHA of the change under preview.
	// +optional
	CommitSHA string `json:"commitSHA,omitempty"`

	// FailedSuite is the test suite that failed, when the failure is test-related.
	// +optional
	FailedSuite string `json:"failedSuite,omitempty"`

	// FailedTest is the individual test that failed, when known.
	// +optional
	FailedTest string `json:"failedTest,omitempty"`
}

// FailureReportPhase is the lifecycle phase of a FailureReport.
// +kubebuilder:validation:Enum=Pending;Collecting;Captured;Persisted;Failed
type FailureReportPhase string

const (
	FailureReportPhasePending    FailureReportPhase = "Pending"
	FailureReportPhaseCollecting FailureReportPhase = "Collecting"
	FailureReportPhaseCaptured   FailureReportPhase = "Captured"
	FailureReportPhasePersisted  FailureReportPhase = "Persisted"
	FailureReportPhaseFailed     FailureReportPhase = "Failed"
)

// FailureReportStatus holds the captured failure evidence bundle and diagnosis.
type FailureReportStatus struct {
	// Phase is the lifecycle phase of the report.
	// +optional
	Phase FailureReportPhase `json:"phase,omitempty"`

	// FailureDetectedAt is when the failure was detected by the operator.
	// +optional
	FailureDetectedAt *metav1.Time `json:"failureDetectedAt,omitempty"`

	// DiagnosisAvailableAt is when a diagnosis was first attached to this report.
	// With FailureDetectedAt it yields the time-to-diagnosis measured by RQ3.
	// +optional
	DiagnosisAvailableAt *metav1.Time `json:"diagnosisAvailableAt,omitempty"`

	// TimeToDiagnosisMillis is DiagnosisAvailableAt - FailureDetectedAt in
	// milliseconds. It is zero until a diagnosis is available (RQ3).
	// +optional
	TimeToDiagnosisMillis int64 `json:"timeToDiagnosisMillis,omitempty"`

	// EvidenceLevel records the evidence configuration (C1..C5) used to produce
	// this report, so each report self-documents which comparison configuration
	// of the evaluation it belongs to. Empty means the full default capability.
	// +optional
	EvidenceLevel string `json:"evidenceLevel,omitempty"`

	// CollectionDurationMillis is the wall-clock time the operator spent
	// assembling the evidence bundle — the in-process collection overhead
	// measured by RQ5.
	// +optional
	CollectionDurationMillis int64 `json:"collectionDurationMillis,omitempty"`

	// BundleSizeBytes is the JSON-serialised size of the evidence items — the
	// storage footprint measured by RQ5.
	// +optional
	BundleSizeBytes int64 `json:"bundleSizeBytes,omitempty"`

	// EvidenceSummary is a short human-readable summary of the captured evidence.
	// +optional
	EvidenceSummary string `json:"evidenceSummary,omitempty"`

	// EvidenceItems is the typed, addressable evidence bundle.
	// +optional
	EvidenceItems []FailureEvidenceItem `json:"evidenceItems,omitempty"`

	// Diagnosis is the probable cause grounded in the evidence items.
	// +optional
	Diagnosis *FailureDiagnosis `json:"diagnosis,omitempty"`

	// ProvenanceGraph links change context, resources, evidence, and diagnosis
	// into one auditable W3C PROV-aligned graph (Contribution 2).
	// +optional
	ProvenanceGraph *ProvenanceGraph `json:"provenanceGraph,omitempty"`

	// PreservedAfterTeardown is true once the report is confirmed readable after
	// the preview namespace has been deleted.
	// +optional
	PreservedAfterTeardown bool `json:"preservedAfterTeardown,omitempty"`

	// StorageRef optionally points to an external store holding the full artifact
	// (for example an object-storage URL). Empty when the report is self-contained.
	// +optional
	StorageRef string `json:"storageRef,omitempty"`

	// ObservedGeneration is the FailureReport generation last processed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=fr
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Preview",type=string,JSONPath=`.spec.previewRef.name`
// +kubebuilder:printcolumn:name="PR",type=integer,JSONPath=`.spec.prNumber`
// +kubebuilder:printcolumn:name="Suite",type=string,JSONPath=`.spec.failedSuite`
// +kubebuilder:printcolumn:name="Level",type=string,JSONPath=`.status.evidenceLevel`
// +kubebuilder:printcolumn:name="Component",type=string,JSONPath=`.status.diagnosis.component`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// FailureReport is an auditable, cluster-scoped artifact that preserves the failure
// evidence of an ephemeral preview environment before its namespace is torn down.
// Being cluster-scoped, it survives deletion of the preview namespace.
type FailureReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FailureReportSpec   `json:"spec,omitempty"`
	Status FailureReportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FailureReportList contains a list of FailureReport.
type FailureReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FailureReport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FailureReport{}, &FailureReportList{})
}
