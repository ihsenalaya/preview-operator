package diagnosis

import (
	"context"
	"strings"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/evidence"
)

// RuleDiagnoser is a deterministic, offline diagnostic engine. It matches the
// evidence bundle against an ordered list of rules; the first rule that fires
// determines the diagnosis. Each rule cites the exact evidence items that
// triggered it, so a rule-based diagnosis is grounded by construction — the rule
// engine can never hallucinate. It is therefore the baseline against which the
// LLM engine's Hallucination Rate (RQ4) and accuracy (RQ2) are read.
//
// Because every rule only inspects evidence items actually present in the
// bundle, the rule engine degrades naturally with the evidence level: at C1
// (logs only) the event- and test-based rules cannot fire. This is the intended
// behaviour for the RQ2 comparison across C1..C5.
type RuleDiagnoser struct{}

// NewRuleDiagnoser returns a rule-based Diagnoser.
func NewRuleDiagnoser() *RuleDiagnoser { return &RuleDiagnoser{} }

// Diagnose applies the ordered rule set to the bundle. The first matching rule
// wins; if none match, a low-confidence "unknown" diagnosis is returned. A
// rule-based result never has hallucinated references, regardless of mode.
func (RuleDiagnoser) Diagnose(_ context.Context, b *evidence.Bundle) (*Result, error) {
	res := &Result{Engine: EngineRule, Mode: ModeGrounded, EvidenceLevel: string(b.Level)}

	for _, r := range orderedRules {
		hit := r.match(b)
		if hit == nil {
			continue
		}
		res.Diagnosis = &platformv1alpha1.FailureDiagnosis{
			ProbableCause:   hit.probableCause,
			Component:       hit.component,
			Category:        hit.category,
			Confidence:      hit.confidence,
			EvidenceRefs:    dedupe(hit.refs),
			Recommendations: hit.recommendations,
		}
		return res, nil
	}

	res.Diagnosis = &platformv1alpha1.FailureDiagnosis{
		ProbableCause: "Insufficient evidence to determine a probable root cause.",
		Component:     "unknown",
		Category:      platformv1alpha1.FailureCategoryUnknown,
		Confidence:    "low",
	}
	return res, nil
}

// ruleHit is what a fired rule contributes to the diagnosis.
type ruleHit struct {
	component       string
	category        platformv1alpha1.FailureCategory
	probableCause   string
	confidence      string
	refs            []string
	recommendations []string
}

// rule pairs a name with a matcher. The matcher returns nil when the rule does
// not apply.
type rule struct {
	name  string
	match func(b *evidence.Bundle) *ruleHit
}

// orderedRules is the rule set, most specific first. The ordering is what makes
// a single bundle resolve to one root cause: e.g. a slow-handler trace (F8) is
// checked before the generic e2e-failure rules so latency is not misread as a
// frontend change.
var orderedRules = []rule{
	{"invalid-migration", ruleInvalidMigration},
	{"image-pull", ruleImagePull},
	{"missing-config", ruleMissingConfig},
	{"db-readiness", ruleDBReadiness},
	{"service-selector", ruleServiceSelector},
	{"contract-break", ruleContractBreak},
	{"latency-timeout", ruleLatencyTimeout},
	{"frontend-break", ruleFrontendBreak},
	{"seed-data", ruleSeedData},
	{"flaky-test", ruleFlakyTest},
}

// --- F1: invalid SQL migration --------------------------------------------

func ruleInvalidMigration(b *evidence.Bundle) *ruleHit {
	sqlError := firstItem(b, func(it item) bool {
		return (it.Type == platformv1alpha1.EvidenceTypeJobLog || it.Type == platformv1alpha1.EvidenceTypePodLog) &&
			containsAny(it.Message, "syntax error", "psycopg2", "programmingerror", "operationalerror", "sqlalchemy", "alembic")
	})
	migrationSignal := firstItem(b, func(it item) bool {
		return (it.Type == platformv1alpha1.EvidenceTypeTestResult || it.Type == platformv1alpha1.EvidenceTypeJobLog) &&
			containsAny(it.Resource, "migration") && isFailureItem(it)
	})
	if sqlError == nil {
		return nil
	}
	refs := []string{sqlError.ID}
	conf := "medium"
	if migrationSignal != nil {
		refs = append(refs, migrationSignal.ID)
		conf = "high"
	}
	refs = append(refs, changedFileRefs(b, "migration", ".sql")...)
	return &ruleHit{
		component:     "migration-job",
		category:      platformv1alpha1.FailureCategoryDatabase,
		probableCause: "The database migration job failed because of an invalid SQL statement.",
		confidence:    conf,
		refs:          refs,
		recommendations: []string{
			"Review the SQL in the changed migration file for the reported syntax error.",
			"Run the migration against a local PostgreSQL instance before re-submitting.",
		},
	}
}

// --- F3: invalid container image tag --------------------------------------

func ruleImagePull(b *evidence.Bundle) *ruleHit {
	ev := firstItem(b, func(it item) bool {
		return it.Type == platformv1alpha1.EvidenceTypeKubernetesEvent &&
			containsAny(it.Message, "imagepullbackoff", "errimagepull", "failed to pull image", "not found: manifest unknown")
	})
	if ev == nil {
		return nil
	}
	return &ruleHit{
		component:     "app-deployment",
		category:      platformv1alpha1.FailureCategoryInfrastructure,
		probableCause: "The container image tag does not exist and could not be pulled.",
		confidence:    "high",
		refs:          []string{ev.ID},
		recommendations: []string{
			"Verify the image reference and tag exist in the registry.",
			"Check the image-build step of the pull request produced and pushed the tag.",
		},
	}
}

// --- F2: missing environment variable -------------------------------------

func ruleMissingConfig(b *evidence.Bundle) *ruleHit {
	crash := firstItem(b, func(it item) bool {
		return containsAny(it.Message, "crashloopbackoff") ||
			(it.Type == platformv1alpha1.EvidenceTypePreviewCondition && containsAny(it.Message, "crashloop"))
	})
	missing := firstItem(b, func(it item) bool {
		return (it.Type == platformv1alpha1.EvidenceTypePodLog || it.Type == platformv1alpha1.EvidenceTypeJobLog) &&
			containsAny(it.Message, "keyerror", "environment variable", "is not set", "not configured",
				"missing required", "missingconfiguration")
	})
	if missing == nil {
		return nil
	}
	refs := []string{missing.ID}
	conf := "medium"
	if crash != nil {
		refs = append(refs, crash.ID)
		conf = "high"
	}
	refs = append(refs, changedFileRefs(b, "config", "env", ".yaml", ".yml")...)
	return &ruleHit{
		component:     "app-deployment",
		category:      platformv1alpha1.FailureCategoryConfiguration,
		probableCause: "The application crashes on startup because a required environment variable is missing.",
		confidence:    conf,
		refs:          refs,
		recommendations: []string{
			"Restore the missing environment variable in the Deployment, ConfigMap or Secret.",
			"Add a startup check that fails fast with the name of the missing variable.",
		},
	}
}

// --- F6: database readiness timeout ---------------------------------------

func ruleDBReadiness(b *evidence.Bundle) *ruleHit {
	probe := firstItem(b, func(it item) bool {
		return it.Type == platformv1alpha1.EvidenceTypeKubernetesEvent &&
			containsAny(it.Message, "readiness probe failed", "liveness probe failed", "unhealthy")
	})
	connErr := firstItem(b, func(it item) bool {
		return (it.Type == platformv1alpha1.EvidenceTypePodLog || it.Type == platformv1alpha1.EvidenceTypeJobLog) &&
			containsAny(it.Message, "connection refused", "could not connect", "could not translate host",
				"the database system is starting up", "connection timed out", "operationalerror")
	})
	// Need at least the connection error; the probe event corroborates it.
	if connErr == nil {
		return nil
	}
	refs := []string{connErr.ID}
	conf := "medium"
	if probe != nil {
		refs = append(refs, probe.ID)
		conf = "high"
	}
	return &ruleHit{
		component:     "app-deployment",
		category:      platformv1alpha1.FailureCategoryInfrastructure,
		probableCause: "The application started before the database was ready and timed out connecting to it.",
		confidence:    conf,
		refs:          refs,
		recommendations: []string{
			"Add an init container or readiness gate that waits for the database to accept connections.",
			"Make the application retry the initial database connection with backoff.",
		},
	}
}

// --- F7: broken Service selector ------------------------------------------

func ruleServiceSelector(b *evidence.Bundle) *ruleHit {
	endpoints := firstItem(b, func(it item) bool {
		return containsAny(it.Message, "no endpoints", "endpoints not available",
			"failed to find any endpoints", "no active endpoints")
	})
	if endpoints == nil {
		return nil
	}
	refs := []string{endpoints.ID}
	refs = append(refs, changedFileRefs(b, "service", "svc")...)
	return &ruleHit{
		component:     "service",
		category:      platformv1alpha1.FailureCategoryInfrastructure,
		probableCause: "The Service selector does not match the application pods, so the Service has no endpoints.",
		confidence:    "high",
		refs:          refs,
		recommendations: []string{
			"Align the Service selector labels with the pod template labels.",
			"Check the endpoints with `kubectl get endpoints` for the affected Service.",
		},
	}
}

// --- F4: removed or broken API endpoint -----------------------------------

func ruleContractBreak(b *evidence.Bundle) *ruleHit {
	contract := failedSuite(b, "contract")
	if contract == nil {
		return nil
	}
	refs := []string{contract.ID}
	refs = append(refs, changedFileRefs(b, "backend", "api", "route", "controller", "handler")...)
	conf := "medium"
	if len(refs) > 1 {
		conf = "high"
	}
	return &ruleHit{
		component:     "backend",
		category:      platformv1alpha1.FailureCategoryApplication,
		probableCause: "A backend API change broke a contract-tested endpoint.",
		confidence:    conf,
		refs:          refs,
		recommendations: []string{
			"Compare the changed backend route or handler against the contract test expectations.",
			"Restore the endpoint contract or update the consumer and contract together.",
		},
	}
}

// --- F8: artificial latency / timeout -------------------------------------

func ruleLatencyTimeout(b *evidence.Bundle) *ruleHit {
	span := firstItem(b, func(it item) bool {
		return it.Type == platformv1alpha1.EvidenceTypeTraceSpan &&
			containsAny(it.Message, "slow", "latency", "duration", "exceeded")
	})
	timeoutLog := firstItem(b, func(it item) bool {
		return (it.Type == platformv1alpha1.EvidenceTypePodLog || it.Type == platformv1alpha1.EvidenceTypeTestResult) &&
			containsAny(it.Message, "timeout", "timed out", "deadline exceeded")
	})
	if span == nil && timeoutLog == nil {
		return nil
	}
	// Distinguish latency from a hard infrastructure failure: latency keeps the
	// pods healthy, so there must be no crash/probe event.
	if hasInfraSymptom(b) {
		return nil
	}
	var refs []string
	conf := "medium"
	if span != nil {
		refs = append(refs, span.ID)
		conf = "high"
	}
	if timeoutLog != nil {
		refs = append(refs, timeoutLog.ID)
	}
	if len(refs) == 0 {
		return nil
	}
	return &ruleHit{
		component:     "backend",
		category:      platformv1alpha1.FailureCategoryObservability,
		probableCause: "A slow backend operation exceeded the request timeout.",
		confidence:    conf,
		refs:          refs,
		recommendations: []string{
			"Profile the slow backend handler using the long-duration trace span.",
			"Either speed up the operation or raise the client/test timeout deliberately.",
		},
	}
}

// --- F5: frontend breaking change -----------------------------------------

func ruleFrontendBreak(b *evidence.Bundle) *ruleHit {
	e2e := failedSuite(b, "e2e")
	if e2e == nil {
		return nil
	}
	frontendFiles := changedFileRefs(b, "frontend", "ui", ".js", ".ts", ".jsx", ".tsx", ".html", ".css")
	if len(frontendFiles) == 0 {
		return nil
	}
	refs := append([]string{e2e.ID}, frontendFiles...)
	return &ruleHit{
		component:     "frontend",
		category:      platformv1alpha1.FailureCategoryApplication,
		probableCause: "A frontend change broke an end-to-end user flow.",
		confidence:    "high",
		refs:          refs,
		recommendations: []string{
			"Check the changed frontend file for a renamed selector, data-testid or route.",
			"Update the end-to-end test and the frontend together.",
		},
	}
}

// --- F9: incorrect seed data ----------------------------------------------

func ruleSeedData(b *evidence.Bundle) *ruleHit {
	seedLog := firstItem(b, func(it item) bool {
		return (it.Type == platformv1alpha1.EvidenceTypeJobLog || it.Type == platformv1alpha1.EvidenceTypePodLog) &&
			containsAny(it.Resource, "seed")
	})
	regression := failedSuite(b, "regression")
	if seedLog == nil && regression == nil {
		return nil
	}
	var refs []string
	if seedLog != nil {
		refs = append(refs, seedLog.ID)
	}
	if regression != nil {
		refs = append(refs, regression.ID)
	}
	conf := "medium"
	if seedLog != nil && regression != nil {
		conf = "high"
	}
	return &ruleHit{
		component:     "seed-job",
		category:      platformv1alpha1.FailureCategoryDatabase,
		probableCause: "Required seed data is missing or malformed, failing a regression test.",
		confidence:    conf,
		refs:          refs,
		recommendations: []string{
			"Inspect the seed job output for the missing or malformed record.",
			"Make the regression test assert on the seed precondition explicitly.",
		},
	}
}

// --- F10: flaky test (negative control) -----------------------------------

func ruleFlakyTest(b *evidence.Bundle) *ruleHit {
	failed := firstItem(b, func(it item) bool {
		return it.Type == platformv1alpha1.EvidenceTypeTestResult && isFailureItem(it)
	})
	if failed == nil {
		return nil
	}
	// Only call it flaky when nothing else explains the failure: no
	// infrastructure symptom and no obvious application/database signal. This is
	// the F10 negative control for RQ4 — the engine must not invent a cause.
	if hasInfraSymptom(b) || hasErrorLog(b) {
		return nil
	}
	return &ruleHit{
		component:     "test-suite",
		category:      platformv1alpha1.FailureCategoryTestReliability,
		probableCause: "A test failed with no corroborating infrastructure or application evidence; the test is likely flaky.",
		confidence:    "low",
		refs:          []string{failed.ID},
		recommendations: []string{
			"Re-run the failed suite; an intermittent pass/fail pattern confirms flakiness.",
			"Remove the non-deterministic behaviour (unseeded randomness, timing race) from the test.",
		},
	}
}

// --- shared predicates -----------------------------------------------------

// item is an alias kept short for the predicate closures above.
type item = platformv1alpha1.FailureEvidenceItem

// firstItem returns a pointer to the first bundle item matching pred, or nil.
// Items are scanned in the bundle's deterministic (ID-sorted) order.
func firstItem(b *evidence.Bundle, pred func(item) bool) *item {
	for _, it := range b.Items() {
		if pred(it) {
			it := it
			return &it
		}
	}
	return nil
}

// failedSuite returns the TestResult evidence item for the named suite when it
// represents a failure, or nil.
func failedSuite(b *evidence.Bundle, suite string) *item {
	return firstItem(b, func(it item) bool {
		return it.Type == platformv1alpha1.EvidenceTypeTestResult &&
			strings.EqualFold(it.Resource, suite) && isFailureItem(it)
	})
}

// changedFileRefs returns the IDs of ChangedFile evidence items whose path or
// classification matches any of the given hints.
func changedFileRefs(b *evidence.Bundle, hints ...string) []string {
	var refs []string
	for _, it := range b.Items() {
		if it.Type != platformv1alpha1.EvidenceTypeChangedFile {
			continue
		}
		if containsAny(it.Resource, hints...) || containsAny(it.Message, hints...) {
			refs = append(refs, it.ID)
		}
	}
	return refs
}

// hasInfraSymptom reports whether the bundle contains a hard infrastructure
// symptom (a crash or an image-pull/probe event). Latency and flakiness rules
// use it to avoid misclassifying an infrastructure failure.
func hasInfraSymptom(b *evidence.Bundle) bool {
	return firstItem(b, func(it item) bool {
		return containsAny(it.Message, "crashloopbackoff", "imagepullbackoff", "errimagepull",
			"readiness probe failed", "liveness probe failed", "oomkilled")
	}) != nil
}

// hasErrorLog reports whether the bundle contains a log with an explicit
// application or database error, used to keep the flaky-test rule conservative.
func hasErrorLog(b *evidence.Bundle) bool {
	return firstItem(b, func(it item) bool {
		return (it.Type == platformv1alpha1.EvidenceTypePodLog || it.Type == platformv1alpha1.EvidenceTypeJobLog) &&
			containsAny(it.Message, "traceback", "exception", "error:", "syntax error",
				"connection refused", "keyerror")
	}) != nil
}

// isFailureItem reports whether a test-result evidence item represents a failure.
func isFailureItem(it item) bool {
	return it.Relevance == platformv1alpha1.EvidenceRelevanceHigh ||
		containsAny(it.Message, "phase=failed", "failed=1", "failed=2", "failed=3",
			"failed=4", "failed=5", "backofflimitexceeded")
}

// containsAny reports whether s contains any of subs, case-insensitively.
func containsAny(s string, subs ...string) bool {
	low := strings.ToLower(s)
	for _, sub := range subs {
		if sub != "" && strings.Contains(low, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// dedupe returns refs with duplicates and empty strings removed, order preserved.
func dedupe(refs []string) []string {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(refs))
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}
