package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// emitReconcileEvent appends an append-only ReconcileEvent to the preview's namespace.
// Errors are logged but do not fail the reconcile — events are best-effort.
func (r *PreviewReconciler) emitReconcileEvent(
	ctx context.Context,
	preview *platformv1alpha1.Preview,
	nsName string,
	evType platformv1alpha1.ReconcileEventType,
	testSuite, outcome, message, correlationID string,
) {
	logger := ctrl.LoggerFrom(ctx)

	filePatterns := filePatternsDenormalized(preview)

	now := metav1.Now()
	ev := &platformv1alpha1.ReconcileEvent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-", preview.Name, strings.ToLower(string(evType))),
			Namespace:    nsName,
			Labels: map[string]string{
				"platform.company.io/preview-name": preview.Name,
				"platform.company.io/event-type":   string(evType),
			},
		},
		Spec: platformv1alpha1.ReconcileEventSpec{
			PreviewRef: corev1.ObjectReference{
				APIVersion: "platform.company.io/v1alpha1",
				Kind:       "Preview",
				Name:       preview.Name,
			},
			Type:          evType,
			TestSuite:     testSuite,
			Outcome:       outcome,
			Message:       message,
			FilePatterns:  filePatterns,
			OccurredAt:    &now,
			CorrelationID: correlationID,
		},
	}

	if err := r.Create(ctx, ev); err != nil {
		logger.Error(err, "failed to emit ReconcileEvent",
			"previewName", preview.Name,
			"eventType", evType,
		)
	}
}

// filePatternsDenormalized extracts changed file paths from ChangeContext for fast queries.
func filePatternsDenormalized(preview *platformv1alpha1.Preview) []string {
	if preview.Spec.ChangeContext == nil {
		return nil
	}
	paths := make([]string, 0, len(preview.Spec.ChangeContext.ChangedFiles))
	for _, f := range preview.Spec.ChangeContext.ChangedFiles {
		paths = append(paths, f.Path)
	}
	return paths
}
