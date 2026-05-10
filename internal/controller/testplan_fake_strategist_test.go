package controller

// fakeStrategist is a test helper that simulates the test-strategist-agent by
// immediately filling any Pending TestPlan with a configurable response.
// Use it in tests that need the Auto mode happy-path without a real LLM.
//
// Usage:
//
//	fs := &fakeStrategist{
//	    confidence: 85,
//	    mustRun:    []platformv1alpha1.TestSuite{platformv1alpha1.TestSuiteSmoke},
//	}
//	fs.fill(ctx, t, cl, plan)

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

type fakeStrategist struct {
	confidence int
	mustRun    []platformv1alpha1.TestSuite
	canSkip    []platformv1alpha1.TestSuite
	rationale  string
}

// fill patches an existing TestPlan with a canned agent response.
// It sets spec fields and status.phase=Ready, mirroring what the real agent does.
func (fs *fakeStrategist) fill(ctx context.Context, t *testing.T, cl client.Client, plan *platformv1alpha1.TestPlan) {
	t.Helper()

	confidence := fs.confidence
	if confidence == 0 {
		confidence = 85
	}
	rationale := fs.rationale
	if rationale == "" {
		rationale = "fake strategist — CI mode"
	}

	mustRun := make([]platformv1alpha1.TestSelector, 0, len(fs.mustRun))
	for _, s := range fs.mustRun {
		mustRun = append(mustRun, platformv1alpha1.TestSelector{
			Suite:  s,
			Name:   "*",
			Reason: "selected by fake strategist",
		})
	}
	if len(mustRun) == 0 {
		mustRun = []platformv1alpha1.TestSelector{
			{Suite: platformv1alpha1.TestSuiteSmoke, Name: "*", Reason: "default: smoke always runs"},
		}
	}

	canSkip := make([]platformv1alpha1.TestSelector, 0, len(fs.canSkip))
	for _, s := range fs.canSkip {
		canSkip = append(canSkip, platformv1alpha1.TestSelector{
			Suite:  s,
			Name:   "*",
			Reason: "skipped by fake strategist",
		})
	}

	now := metav1.Now()

	// Fetch the latest version before patching to avoid ResourceVersion conflicts.
	var latest platformv1alpha1.TestPlan
	if err := cl.Get(ctx, types.NamespacedName{Name: plan.Name, Namespace: plan.Namespace}, &latest); err != nil {
		t.Fatalf("fakeStrategist: fetch plan %s: %v", plan.Name, err)
	}

	latest.Spec.GeneratedBy = platformv1alpha1.TestPlanByAgent
	latest.Spec.AgentName = "fake-strategist"
	latest.Spec.Confidence = confidence
	latest.Spec.MustRun = mustRun
	latest.Spec.CanSkip = canSkip
	latest.Spec.Rationale = rationale
	latest.Spec.GeneratedAt = &now
	if err := cl.Update(ctx, &latest); err != nil {
		t.Fatalf("fakeStrategist: update plan spec %s: %v", plan.Name, err)
	}

	// Promote status to Ready (the real agent does the same via status subresource patch).
	latest.Status.Phase = platformv1alpha1.TestPlanPhaseReady
	if err := cl.Status().Update(ctx, &latest); err != nil {
		t.Fatalf("fakeStrategist: update plan status %s: %v", plan.Name, err)
	}
}

// findPendingPlan returns the first Pending TestPlan for a Preview, polling until
// the plan appears or the deadline expires.
func findPendingPlan(ctx context.Context, t *testing.T, cl client.Client, previewName, nsName string) *platformv1alpha1.TestPlan {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list := &platformv1alpha1.TestPlanList{}
		if err := cl.List(ctx, list, client.InNamespace(nsName)); err != nil {
			t.Fatalf("list TestPlans: %v", err)
		}
		for i := range list.Items {
			p := &list.Items[i]
			if p.Labels["platform.company.io/preview-name"] == previewName &&
				p.Status.Phase == platformv1alpha1.TestPlanPhasePending {
				return p
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no Pending TestPlan found for preview %q within 5s", previewName)
	return nil
}

// TestFakeStrategist_HappyPath is an integration test that runs the full
// mode=Auto flow: controller creates stub → fake strategist fills it →
// controller accepts it on the next reconcile.
func TestFakeStrategist_HappyPath(t *testing.T) {
	scheme := testAIScheme(t)
	nsName := "preview-pr-fake"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	preview := makeTestPreview("pr-fake", platformv1alpha1.TestStrategyAuto)

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(preview, &platformv1alpha1.TestPlan{}).
		WithObjects(preview, ns).
		Build()

	r := &PreviewReconciler{Client: cl, Scheme: scheme}
	ctx := context.Background()

	// First reconcile: stub created, returns RequeueAfter.
	result, plan, err := r.reconcileTestStrategy(ctx, preview, nsName)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if plan != nil {
		t.Fatalf("expected nil plan on first reconcile, got %v", plan.Name)
	}
	if result.RequeueAfter == 0 && !result.Requeue {
		t.Error("expected requeue after stub creation")
	}

	// Fake strategist fills the stub.
	stub := findPendingPlan(ctx, t, cl, "pr-fake", nsName)
	fs := &fakeStrategist{
		confidence: 90,
		mustRun:    []platformv1alpha1.TestSuite{platformv1alpha1.TestSuiteSmoke, platformv1alpha1.TestSuiteRegression},
		canSkip:    []platformv1alpha1.TestSuite{platformv1alpha1.TestSuiteContract, platformv1alpha1.TestSuiteMigration},
		rationale:  "backend-only change, no migration or contract files touched",
	}
	fs.fill(ctx, t, cl, stub)

	// Second reconcile: plan ready → accepted.
	result2, plan2, err := r.reconcileTestStrategy(ctx, preview, nsName)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if result2.RequeueAfter > 0 {
		t.Errorf("expected empty result on second reconcile, got RequeueAfter=%v", result2.RequeueAfter)
	}
	if plan2 == nil {
		t.Fatal("expected accepted plan on second reconcile, got nil")
	}
	if plan2.Spec.GeneratedBy != platformv1alpha1.TestPlanByAgent {
		t.Errorf("expected agent plan, got %q", plan2.Spec.GeneratedBy)
	}
	if plan2.Spec.Confidence != 90 {
		t.Errorf("confidence = %d, want 90", plan2.Spec.Confidence)
	}
	if preview.Status.TestPlanResolution == nil {
		t.Fatal("expected testPlanResolution set")
	}
	if preview.Status.TestPlanResolution.Source != platformv1alpha1.TestPlanSourceAgent {
		t.Errorf("resolution source = %q, want Agent", preview.Status.TestPlanResolution.Source)
	}
}
