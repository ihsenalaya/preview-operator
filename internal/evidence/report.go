package evidence

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// failureReportSuffix is appended to a Preview name to form the deterministic
// FailureReport name.
const failureReportSuffix = "-failure"

// FailureReportName returns the deterministic name of the FailureReport for a
// Preview. A deterministic name is what makes report generation idempotent:
// repeated reconciliation create-or-updates the same object rather than creating
// a new one each time (the Reconcile Idempotency metric, metrics.md §11).
func FailureReportName(previewName string) string {
	return previewName + failureReportSuffix
}

// BuildFailureReport produces a FailureReport custom resource from a bundle.
//
// The result is deterministic for a given bundle: the object name is fixed, the
// evidence items are sorted by ID, and the diagnosis is copied as-is. Applying the
// result twice therefore yields the same object — no duplicate reports, no
// duplicate evidence items.
//
// Status is populated so callers can apply it via the status subresource; the
// FailureDetectedAt timestamp is the only non-deterministic field.
func BuildFailureReport(b *Bundle) *platformv1alpha1.FailureReport {
	if b == nil {
		return nil
	}
	items := b.Items()

	detectedAt := b.FailureDetectedAt
	status := platformv1alpha1.FailureReportStatus{
		Phase:             platformv1alpha1.FailureReportPhaseCaptured,
		FailureDetectedAt: &detectedAt,
		EvidenceSummary:   b.Summary(),
		EvidenceItems:     items,
		Diagnosis:         b.diagnosis,
		EvidenceLevel:     string(b.Level),
		BundleSizeBytes:   bundleSizeBytes(items),
	}
	if b.CollectionDuration > 0 {
		status.CollectionDurationMillis = b.CollectionDuration.Milliseconds()
	}

	// The provenance graph is materialised only at C5. An unset Level means the
	// full default capability, so the graph is built there too — this keeps the
	// behaviour unchanged for callers that do not select a level.
	if b.Level == "" || b.Level.IncludesProvenanceGraph() {
		status.ProvenanceGraph = BuildProvenanceGraph(b)
	}

	// Diagnosis timing: DiagnosisAvailableAt with FailureDetectedAt yields the
	// time-to-diagnosis measured by RQ3.
	if b.diagnosis != nil {
		availableAt := b.diagnosisAt
		if availableAt.IsZero() {
			availableAt = metav1.Now()
		}
		status.DiagnosisAvailableAt = &availableAt
		if !detectedAt.IsZero() {
			if d := availableAt.Sub(detectedAt.Time); d > 0 {
				status.TimeToDiagnosisMillis = d.Milliseconds()
			}
		}
	}

	return &platformv1alpha1.FailureReport{
		ObjectMeta: metav1.ObjectMeta{
			Name: FailureReportName(b.PreviewName),
			Labels: map[string]string{
				"platform.company.io/preview": b.PreviewName,
			},
		},
		Spec: platformv1alpha1.FailureReportSpec{
			PreviewRef: corev1.ObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "Preview",
				Name:       b.PreviewName,
			},
			Namespace:   b.Namespace,
			PRNumber:    b.PRNumber,
			CommitSHA:   b.CommitSHA,
			FailedSuite: b.FailedSuite,
			FailedTest:  b.FailedTest,
		},
		Status: status,
	}
}

// bundleSizeBytes returns the JSON-serialised size of the evidence items — the
// storage footprint measured by RQ5. A marshalling error (which the typed items
// cannot realistically produce) yields 0 rather than failing report generation.
func bundleSizeBytes(items []platformv1alpha1.FailureEvidenceItem) int64 {
	data, err := json.Marshal(items)
	if err != nil {
		return 0
	}
	return int64(len(data))
}
