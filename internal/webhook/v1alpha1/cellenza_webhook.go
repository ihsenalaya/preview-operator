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

func SetupCellenzaWebhookWithManager(mgr ctrl.Manager) error {
	return SetupWebhookWithManager(mgr)
}

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
	if r.Spec.GitHub != nil && r.Spec.GitHub.TokenSecretRef != nil && r.Spec.GitHub.TokenSecretRef.Key == "" {
		r.Spec.GitHub.TokenSecretRef.Key = "token"
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
	if err := validateTelemetry(r); err != nil {
		return nil, err
	}
	if err := validateGitHub(r); err != nil {
		return nil, err
	}
	var warnings admission.Warnings
	if r.Spec.Replicas > 3 {
		warnings = append(warnings, fmt.Sprintf(
			"running %d replicas in a preview env is expensive", r.Spec.Replicas,
		))
	}
	return warnings, nil
}

func validateTelemetry(r *apiv1alpha1.Cellenza) error {
	if r.Spec.Telemetry == nil || !r.Spec.Telemetry.Enabled {
		return nil
	}

	if r.Spec.Telemetry.AutoInstrumentation == nil {
		return fmt.Errorf("spec.telemetry.autoInstrumentation is required when spec.telemetry.enabled is true")
	}

	auto := r.Spec.Telemetry.AutoInstrumentation
	switch auto.Language {
	case apiv1alpha1.TelemetryLanguagePython,
		apiv1alpha1.TelemetryLanguageJava,
		apiv1alpha1.TelemetryLanguageNodeJS,
		apiv1alpha1.TelemetryLanguageDotNet,
		apiv1alpha1.TelemetryLanguageGo,
		apiv1alpha1.TelemetryLanguageSDK:
	default:
		return fmt.Errorf("spec.telemetry.autoInstrumentation.language must be one of python, java, nodejs, dotnet, go, sdk")
	}

	if auto.Language == apiv1alpha1.TelemetryLanguageGo && auto.GoTargetExecutable == "" {
		return fmt.Errorf("spec.telemetry.autoInstrumentation.goTargetExecutable is required for Go auto-instrumentation")
	}
	if auto.Language != apiv1alpha1.TelemetryLanguageGo && auto.GoTargetExecutable != "" {
		return fmt.Errorf("spec.telemetry.autoInstrumentation.goTargetExecutable is only valid for Go auto-instrumentation")
	}
	if auto.Language != apiv1alpha1.TelemetryLanguagePython && auto.PythonPlatform != "" {
		return fmt.Errorf("spec.telemetry.autoInstrumentation.pythonPlatform is only valid for Python auto-instrumentation")
	}

	return nil
}

func validateGitHub(r *apiv1alpha1.Cellenza) error {
	if r.Spec.GitHub == nil || !r.Spec.GitHub.Enabled {
		return nil
	}
	if r.Spec.GitHub.Owner == "" {
		return fmt.Errorf("spec.github.owner is required when GitHub integration is enabled")
	}
	if r.Spec.GitHub.Repo == "" {
		return fmt.Errorf("spec.github.repo is required when GitHub integration is enabled")
	}
	if r.Spec.GitHub.DeploymentID <= 0 {
		return fmt.Errorf("spec.github.deploymentId must be positive when GitHub integration is enabled")
	}
	if r.Spec.GitHub.TokenSecretRef == nil || r.Spec.GitHub.TokenSecretRef.Name == "" {
		return fmt.Errorf("spec.github.tokenSecretRef.name is required when GitHub integration is enabled")
	}
	return nil
}
