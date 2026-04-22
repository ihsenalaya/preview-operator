package v1alpha1

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
)

var cellenzalog = logf.Log.WithName("cellenza-webhook")

func SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &apiv1alpha1.Cellenza{}).
		WithDefaulter(&CellenzaCustomDefaulter{}).
		WithValidator(&CellenzaCustomValidator{}).
		Complete()
}

type CellenzaCustomDefaulter struct{}

func (d *CellenzaCustomDefaulter) Default(ctx context.Context, r *apiv1alpha1.Cellenza) error {
	cellenzalog.Info("defaulting Cellenza", "name", r.Name)
	if r.Spec.TTL == "" {
		r.Spec.TTL = "48h"
	}
	if r.Spec.ResourceTier == "" {
		r.Spec.ResourceTier = apiv1alpha1.TierMedium
	}
	if r.Spec.Replicas == 0 {
		r.Spec.Replicas = 1
	}
	if r.Spec.ResourceTier == apiv1alpha1.TierLarge {
		r.Spec.RequiresApproval = true
	}
	return nil
}

type CellenzaCustomValidator struct{}

func (v *CellenzaCustomValidator) ValidateCreate(ctx context.Context, r *apiv1alpha1.Cellenza) (admission.Warnings, error) {
	cellenzalog.Info("validate create", "name", r.Name)
	return validateCellenza(r)
}

func (v *CellenzaCustomValidator) ValidateUpdate(ctx context.Context, old, new *apiv1alpha1.Cellenza) (admission.Warnings, error) {
	cellenzalog.Info("validate update", "name", new.Name)
	return validateCellenza(new)
}

func (v *CellenzaCustomValidator) ValidateDelete(ctx context.Context, r *apiv1alpha1.Cellenza) (admission.Warnings, error) {
	return nil, nil
}

func validateCellenza(r *apiv1alpha1.Cellenza) (admission.Warnings, error) {
	if r.Spec.ResourceTier == apiv1alpha1.TierLarge && r.Spec.ApprovedBy == "" {
		return nil, fmt.Errorf("resourceTier 'large' requires spec.approvedBy")
	}
	if r.Spec.RequiresApproval && r.Spec.ApprovedBy == "" {
		return nil, fmt.Errorf("spec.requiresApproval is true but spec.approvedBy is not set")
	}
	if r.Spec.PRNumber <= 0 {
		return nil, fmt.Errorf("spec.prNumber must be positive, got %d", r.Spec.PRNumber)
	}
	if r.Spec.Image == "latest" || len(r.Spec.Image) < 3 {
		return nil, fmt.Errorf("spec.image must be a fully qualified image reference")
	}
	var warnings admission.Warnings
	if r.Spec.Replicas > 3 {
		warnings = append(warnings, fmt.Sprintf(
			"running %d replicas in a preview env is expensive", r.Spec.Replicas,
		))
	}
	return warnings, nil
}
