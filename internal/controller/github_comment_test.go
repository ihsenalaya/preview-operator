package controller

import (
	"strings"
	"testing"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

func TestBuildTestResultsCommentBodyIncludesAIEnrichment(t *testing.T) {
	c := &platformv1alpha1.Preview{
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{Enabled: true},
		},
		Status: platformv1alpha1.PreviewStatus{
			URL: "http://pr-34.preview.localtest.me:8080",
			Tests: &platformv1alpha1.TestSuiteStatus{
				Phase: phaseFailed,
				Smoke: platformv1alpha1.TestResult{Phase: phaseSucceeded, Passed: 2},
				Regression: platformv1alpha1.TestResult{
					Phase:  phaseFailed,
					Passed: 7,
					Failed: 2,
					Output: []string{"FAIL regression product_detail: expected at least one product in /api/products"},
				},
				E2E: platformv1alpha1.TestResult{Phase: phaseFailed, Passed: 2, Failed: 1},
			},
			AIEnrichment: &platformv1alpha1.AIEnrichmentStatus{
				Phase:       phaseFailed,
				SeedStatus:  phaseSucceeded,
				TestsStatus: phaseFailed,
				TestResults: []string{"FAIL POST /api/categories - Unexpected status code 500"},
				Error:       "AI job preview-pr-34/ai-tests failed: Job has reached the specified backoff limit",
			},
		},
	}

	body := buildTestResultsCommentBody(c, nil)
	for _, want := range []string{
		"Test Suite Results",
		"### AI Enrichment",
		"- Seed: SUCCESS `Succeeded`",
		"- Tests: FAIL `Failed`",
		"`FAIL POST /api/categories - Unexpected status code 500`",
		"Relancer avec `@preview enrich pr-N`",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("buildTestResultsCommentBody missing %q:\n%s", want, body)
		}
	}
}
