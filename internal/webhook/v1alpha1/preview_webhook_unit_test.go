package v1alpha1

import (
	"context"
	"testing"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

func TestPreviewDefaulterDefaultsAIEnrichmentTasks(t *testing.T) {
	defaulter := &PreviewCustomDefaulter{}
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
			},
		},
	}

	if err := defaulter.Default(context.Background(), c); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if c.Spec.AIEnrichment.Seed == nil || !c.Spec.AIEnrichment.Seed.Enabled {
		t.Fatalf("expected seed task to be defaulted to enabled, got %#v", c.Spec.AIEnrichment.Seed)
	}
	if c.Spec.AIEnrichment.Tests == nil || !c.Spec.AIEnrichment.Tests.Enabled {
		t.Fatalf("expected tests task to be defaulted to enabled, got %#v", c.Spec.AIEnrichment.Tests)
	}
}

func TestPreviewDefaulterKeepsExplicitAIDisables(t *testing.T) {
	defaulter := &PreviewCustomDefaulter{}
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled: true,
				Seed:    &platformv1alpha1.AIEnrichmentTaskSpec{Enabled: false},
				Tests:   &platformv1alpha1.AIEnrichmentTaskSpec{Enabled: false},
			},
		},
	}

	if err := defaulter.Default(context.Background(), c); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if c.Spec.AIEnrichment.Seed == nil || c.Spec.AIEnrichment.Seed.Enabled {
		t.Fatalf("expected explicit seed=false to be preserved, got %#v", c.Spec.AIEnrichment.Seed)
	}
	if c.Spec.AIEnrichment.Tests == nil || c.Spec.AIEnrichment.Tests.Enabled {
		t.Fatalf("expected explicit tests=false to be preserved, got %#v", c.Spec.AIEnrichment.Tests)
	}
}
