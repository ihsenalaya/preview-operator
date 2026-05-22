package diagnosis

import (
	"context"
	"testing"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/evidence"
)

// ev builds an evidence item with an explicit ID so tests can assert on
// EvidenceRefs deterministically.
func ev(id string, t platformv1alpha1.EvidenceType, resource, message string, rel platformv1alpha1.EvidenceRelevance) platformv1alpha1.FailureEvidenceItem {
	return platformv1alpha1.FailureEvidenceItem{ID: id, Type: t, Resource: resource, Message: message, Relevance: rel}
}

// bundleWith builds a bundle from a set of evidence items.
func bundleWith(items ...platformv1alpha1.FailureEvidenceItem) *evidence.Bundle {
	b := evidence.NewBundle(nil)
	for _, it := range items {
		b.Add(it)
	}
	return b
}

// assertGrounded fails the test if any EvidenceRef does not resolve to an item
// in the bundle — the rule engine must be grounded by construction.
func assertGrounded(t *testing.T, res *Result, b *evidence.Bundle) {
	t.Helper()
	if res.Diagnosis == nil {
		t.Fatal("result has no diagnosis")
	}
	if len(res.HallucinatedRefs) != 0 {
		t.Errorf("rule engine produced hallucinated refs: %v", res.HallucinatedRefs)
	}
	if len(res.Diagnosis.EvidenceRefs) == 0 {
		t.Error("diagnosis has no evidence references")
	}
	for _, ref := range res.Diagnosis.EvidenceRefs {
		if !b.Has(ref) {
			t.Errorf("diagnosis references unknown evidence ID %q", ref)
		}
	}
}

func TestRuleDiagnoserScenarios(t *testing.T) {
	high := platformv1alpha1.EvidenceRelevanceHigh

	cases := []struct {
		name          string
		bundle        *evidence.Bundle
		wantComponent string
		wantCategory  platformv1alpha1.FailureCategory
	}{
		{
			name: "F1 invalid migration",
			bundle: bundleWith(
				ev("joblog-mig", platformv1alpha1.EvidenceTypeJobLog, "migration",
					`psycopg2.errors.SyntaxError: syntax error at or near "INDX"`, high),
				ev("test-mig", platformv1alpha1.EvidenceTypeTestResult, "migration",
					"phase=Failed passed=0 failed=1", high),
				ev("file-mig", platformv1alpha1.EvidenceTypeChangedFile, "migrations/versions/002_fault.py",
					"type=DatabaseMigration", high),
			),
			wantComponent: "migration-job",
			wantCategory:  platformv1alpha1.FailureCategoryDatabase,
		},
		{
			name: "F3 image pull",
			bundle: bundleWith(
				ev("ev-img", platformv1alpha1.EvidenceTypeKubernetesEvent, "pod/app",
					`Failed to pull image "app:nope": ImagePullBackOff`, high),
			),
			wantComponent: "app-deployment",
			wantCategory:  platformv1alpha1.FailureCategoryInfrastructure,
		},
		{
			name: "F2 missing config",
			bundle: bundleWith(
				ev("ev-crash", platformv1alpha1.EvidenceTypeKubernetesEvent, "pod/app",
					"Back-off restarting failed container: CrashLoopBackOff", high),
				ev("log-key", platformv1alpha1.EvidenceTypePodLog, "app",
					"KeyError: 'DATABASE_URL' environment variable is not set", high),
			),
			wantComponent: "app-deployment",
			wantCategory:  platformv1alpha1.FailureCategoryConfiguration,
		},
		{
			name: "F6 database readiness timeout",
			bundle: bundleWith(
				ev("ev-probe", platformv1alpha1.EvidenceTypeKubernetesEvent, "pod/app",
					"Readiness probe failed: dial tcp connection refused", high),
				ev("log-conn", platformv1alpha1.EvidenceTypePodLog, "app",
					"could not connect to server: Connection refused", high),
			),
			wantComponent: "app-deployment",
			wantCategory:  platformv1alpha1.FailureCategoryInfrastructure,
		},
		{
			name: "F7 broken service selector",
			bundle: bundleWith(
				ev("ev-ep", platformv1alpha1.EvidenceTypeKubernetesEvent, "svc/app",
					"no endpoints available for service \"app\"", high),
				ev("file-svc", platformv1alpha1.EvidenceTypeChangedFile, "deploy/service.yaml",
					"type=Manifest", high),
			),
			wantComponent: "service",
			wantCategory:  platformv1alpha1.FailureCategoryInfrastructure,
		},
		{
			name: "F4 contract break",
			bundle: bundleWith(
				ev("test-contract", platformv1alpha1.EvidenceTypeTestResult, "contract",
					"phase=Failed passed=3 failed=1", high),
				ev("file-api", platformv1alpha1.EvidenceTypeChangedFile, "app.py",
					"type=Backend", high),
			),
			wantComponent: "backend",
			wantCategory:  platformv1alpha1.FailureCategoryApplication,
		},
		{
			name: "F8 latency timeout",
			bundle: bundleWith(
				ev("span-slow", platformv1alpha1.EvidenceTypeTraceSpan, "GET /api/products",
					"span duration 5200ms exceeded threshold", high),
				ev("test-e2e", platformv1alpha1.EvidenceTypeTestResult, "e2e",
					"phase=Failed passed=2 failed=1 request timed out", high),
			),
			wantComponent: "backend",
			wantCategory:  platformv1alpha1.FailureCategoryObservability,
		},
		{
			name: "F5 frontend break",
			bundle: bundleWith(
				ev("test-e2e", platformv1alpha1.EvidenceTypeTestResult, "e2e",
					"phase=Failed passed=2 failed=1\nE2E: element [data-testid=submit] not found", high),
				ev("file-fe", platformv1alpha1.EvidenceTypeChangedFile, "frontend.py",
					"type=Frontend", high),
			),
			wantComponent: "frontend",
			wantCategory:  platformv1alpha1.FailureCategoryApplication,
		},
		{
			name: "F9 incorrect seed data",
			bundle: bundleWith(
				ev("log-seed", platformv1alpha1.EvidenceTypeJobLog, "seed",
					"seed: inserted 0 of 5 expected records", high),
				ev("test-reg", platformv1alpha1.EvidenceTypeTestResult, "regression",
					"phase=Failed passed=8 failed=1 missing record", high),
			),
			wantComponent: "seed-job",
			wantCategory:  platformv1alpha1.FailureCategoryDatabase,
		},
		{
			name: "F10 flaky test",
			bundle: bundleWith(
				ev("test-e2e", platformv1alpha1.EvidenceTypeTestResult, "e2e",
					"phase=Failed passed=4 failed=1", high),
			),
			wantComponent: "test-suite",
			wantCategory:  platformv1alpha1.FailureCategoryTestReliability,
		},
	}

	rd := NewRuleDiagnoser()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := rd.Diagnose(context.Background(), tc.bundle)
			if err != nil {
				t.Fatalf("Diagnose: %v", err)
			}
			if res.Engine != EngineRule {
				t.Errorf("engine = %q, want rule", res.Engine)
			}
			if res.Diagnosis.Component != tc.wantComponent {
				t.Errorf("component = %q, want %q", res.Diagnosis.Component, tc.wantComponent)
			}
			if res.Diagnosis.Category != tc.wantCategory {
				t.Errorf("category = %q, want %q", res.Diagnosis.Category, tc.wantCategory)
			}
			if len(res.Diagnosis.Recommendations) == 0 {
				t.Error("diagnosis carries no recommendations")
			}
			assertGrounded(t, res, tc.bundle)
		})
	}
}

func TestRuleDiagnoserUnknownOnEmptyBundle(t *testing.T) {
	res, err := NewRuleDiagnoser().Diagnose(context.Background(), bundleWith())
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if res.Diagnosis.Category != platformv1alpha1.FailureCategoryUnknown {
		t.Errorf("category = %q, want unknown", res.Diagnosis.Category)
	}
	if len(res.Diagnosis.EvidenceRefs) != 0 {
		t.Errorf("unknown diagnosis must cite no evidence, got %v", res.Diagnosis.EvidenceRefs)
	}
}

// TestRuleFlakyIsConservative is the F10 negative control: when a real
// infrastructure cause is present the flaky-test rule must not fire.
func TestRuleFlakyIsConservative(t *testing.T) {
	high := platformv1alpha1.EvidenceRelevanceHigh
	b := bundleWith(
		ev("test-e2e", platformv1alpha1.EvidenceTypeTestResult, "e2e",
			"phase=Failed passed=4 failed=1", high),
		ev("ev-img", platformv1alpha1.EvidenceTypeKubernetesEvent, "pod/app",
			`Failed to pull image "app:nope": ImagePullBackOff`, high),
	)
	res, err := NewRuleDiagnoser().Diagnose(context.Background(), b)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if res.Diagnosis.Category == platformv1alpha1.FailureCategoryTestReliability {
		t.Error("flaky-test rule fired despite a real infrastructure cause (ImagePullBackOff)")
	}
}

// TestRuleDegradesWithEvidenceLevel checks the RQ2 premise for the rule engine:
// with only logs (C1) an event-based fault is no longer diagnosable.
func TestRuleDegradesWithEvidenceLevel(t *testing.T) {
	high := platformv1alpha1.EvidenceRelevanceHigh
	imgEvent := ev("ev-img", platformv1alpha1.EvidenceTypeKubernetesEvent, "pod/app",
		`Failed to pull image "app:nope": ImagePullBackOff`, high)

	full := bundleWith(imgEvent)
	logsOnly := bundleWith() // C1 keeps no Kubernetes events

	rd := NewRuleDiagnoser()
	fullRes, _ := rd.Diagnose(context.Background(), full)
	logsRes, _ := rd.Diagnose(context.Background(), logsOnly)

	if fullRes.Diagnosis.Category != platformv1alpha1.FailureCategoryInfrastructure {
		t.Errorf("with the event, category = %q, want infrastructure", fullRes.Diagnosis.Category)
	}
	if logsRes.Diagnosis.Category != platformv1alpha1.FailureCategoryUnknown {
		t.Errorf("without the event, category = %q, want unknown", logsRes.Diagnosis.Category)
	}
}
