package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/evidence"
)

// RBAC for the FailureReport custom resource. These markers are collected by
// controller-gen when `make manifests` regenerates the operator ClusterRole.
//
// +kubebuilder:rbac:groups=platform.company.io,resources=failurereports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.company.io,resources=failurereports/status,verbs=get;update;patch

// EnsureFailureReport assembles a failure evidence bundle from a failed Preview
// at the given evidence level (C1..C5) and persists it as a cluster-scoped
// FailureReport.
//
// It is idempotent: the report name is deterministic (evidence.FailureReportName),
// so repeated reconciliation create-or-updates the same object — no duplicate
// reports and no duplicate evidence items.
//
// The FailureReport is intentionally NOT owned by the Preview: being cluster-scoped
// it already survives preview-namespace teardown (RQ1), and keeping it independent
// of the Preview's lifecycle preserves the audit trail.
//
// EnsureFailureReport is the integration point for evidence preservation. It is
// invoked from the operator's failure paths via captureFailureReport — see
// setFailedStatus (infrastructure failures) and the test-suite failure path.
func EnsureFailureReport(ctx context.Context, c client.Client, preview *platformv1alpha1.Preview, level evidence.Level) (*platformv1alpha1.FailureReport, error) {
	if preview == nil {
		return nil, nil
	}

	bundle := evidence.AssembleBundleForLevel(preview, level)
	desired := evidence.BuildFailureReport(bundle)
	if desired == nil {
		return nil, nil
	}

	existing := &platformv1alpha1.FailureReport{}
	err := c.Get(ctx, types.NamespacedName{Name: desired.Name}, existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := c.Create(ctx, desired); err != nil {
			return nil, err
		}
		if err := c.Status().Update(ctx, desired); err != nil {
			return desired, err
		}
		return desired, nil
	case err != nil:
		return nil, err
	}

	existing.Spec = desired.Spec
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	for k, v := range desired.Labels {
		existing.Labels[k] = v
	}
	if err := c.Update(ctx, existing); err != nil {
		return nil, err
	}

	existing.Status = desired.Status
	if err := c.Status().Update(ctx, existing); err != nil {
		return existing, err
	}
	return existing, nil
}

// captureFailureReport persists a FailureReport for a failed Preview. It is
// best-effort: a capture error is logged but never fails reconciliation, so
// evidence collection can never break the operator's main loop. The FailureReport
// is cluster-scoped, so it survives the eventual teardown of the preview namespace.
//
// When evidence collection is disabled (EVIDENCE_COLLECTION=disabled) this is a
// no-op: that configuration is the overhead baseline measured by RQ5. The
// evidence level (EVIDENCE_LEVEL, C1..C5) selects how much evidence is captured.
func (r *PreviewReconciler) captureFailureReport(ctx context.Context, c *platformv1alpha1.Preview) {
	if c == nil {
		return
	}
	if !r.EvidenceCollection {
		log.FromContext(ctx).V(1).Info("evidence collection disabled; skipping FailureReport", "preview", c.Name)
		return
	}
	level := r.EvidenceLevel
	if level == "" {
		level = evidence.DefaultLevel
	}
	if _, err := EnsureFailureReport(ctx, r.Client, c, level); err != nil {
		log.FromContext(ctx).Error(err, "failed to persist FailureReport", "preview", c.Name)
	}
}
