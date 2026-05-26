package diagnosis

import (
	"context"
	"strings"
	"testing"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// mockLLM is a scripted LLMClient that records the prompts it received.
type mockLLM struct {
	model     string
	reply     string
	err       error
	gotSystem string
	gotUser   string
}

func (m *mockLLM) Model() string { return m.model }

func (m *mockLLM) Complete(_ context.Context, system, user string) (string, error) {
	m.gotSystem, m.gotUser = system, user
	return m.reply, m.err
}

const groundedReply = `{
  "probableCause": "The migration failed on invalid SQL.",
  "component": "migration-job",
  "category": "database",
  "confidence": "high",
  "evidenceRefs": ["joblog-real", "joblog-invented"],
  "recommendations": ["fix the SQL"]
}`

func TestLLMDiagnoserGroundedStripsInventedRefs(t *testing.T) {
	b := bundleWith(
		ev("joblog-real", platformv1alpha1.EvidenceTypeJobLog, "migration", "syntax error", platformv1alpha1.EvidenceRelevanceHigh),
	)
	client := &mockLLM{model: "test-model-1", reply: groundedReply}
	d := NewLLMDiagnoser(client, ModeGrounded)

	res, err := d.Diagnose(context.Background(), b)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if res.Engine != EngineLLM || res.Mode != ModeGrounded {
		t.Errorf("engine/mode = %q/%q", res.Engine, res.Mode)
	}
	if res.Model != "test-model-1" {
		t.Errorf("model = %q, want test-model-1", res.Model)
	}
	if got := res.Diagnosis.EvidenceRefs; len(got) != 1 || got[0] != "joblog-real" {
		t.Errorf("grounded EvidenceRefs = %v, want [joblog-real]", got)
	}
	if got := res.HallucinatedRefs; len(got) != 1 || got[0] != "joblog-invented" {
		t.Errorf("HallucinatedRefs = %v, want [joblog-invented]", got)
	}
}

func TestLLMDiagnoserFreeformKeepsRefsButStillReportsThem(t *testing.T) {
	b := bundleWith(
		ev("joblog-real", platformv1alpha1.EvidenceTypeJobLog, "migration", "syntax error", platformv1alpha1.EvidenceRelevanceHigh),
	)
	client := &mockLLM{model: "test-model-1", reply: groundedReply}
	d := NewLLMDiagnoser(client, ModeFreeform)

	res, err := d.Diagnose(context.Background(), b)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	// Free-form mode does not strip invented references...
	if got := res.Diagnosis.EvidenceRefs; len(got) != 2 {
		t.Errorf("freeform EvidenceRefs = %v, want both kept", got)
	}
	// ...but the invented one is still reported for the RQ4 measurement.
	if got := res.HallucinatedRefs; len(got) != 1 || got[0] != "joblog-invented" {
		t.Errorf("HallucinatedRefs = %v, want [joblog-invented]", got)
	}
}

func TestLLMDiagnoserParsesFencedJSON(t *testing.T) {
	b := bundleWith(ev("podlog-1", platformv1alpha1.EvidenceTypePodLog, "app", "boom", platformv1alpha1.EvidenceRelevanceHigh))
	client := &mockLLM{
		model: "m",
		reply: "Here is the diagnosis:\n```json\n" + groundedReply + "\n```\nDone.",
	}
	res, err := NewLLMDiagnoser(client, ModeGrounded).Diagnose(context.Background(), b)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if res.Diagnosis.Category != platformv1alpha1.FailureCategoryDatabase {
		t.Errorf("category = %q, want database", res.Diagnosis.Category)
	}
}

func TestLLMDiagnoserRejectsNonJSON(t *testing.T) {
	b := bundleWith(ev("podlog-1", platformv1alpha1.EvidenceTypePodLog, "app", "boom", platformv1alpha1.EvidenceRelevanceHigh))
	client := &mockLLM{model: "m", reply: "I cannot produce JSON today."}
	if _, err := NewLLMDiagnoser(client, ModeGrounded).Diagnose(context.Background(), b); err == nil {
		t.Error("expected an error for non-JSON model output")
	}
}

// TestLLMPromptWithholdsIDsInFreeform verifies the RQ4 ablation is real: the
// grounded prompt shows evidence IDs, the free-form prompt must not.
func TestLLMPromptWithholdsIDsInFreeform(t *testing.T) {
	b := bundleWith(ev("podlog-secretid", platformv1alpha1.EvidenceTypePodLog, "app", "boom", platformv1alpha1.EvidenceRelevanceHigh))

	grounded := &mockLLM{model: "m", reply: groundedReply}
	if _, err := NewLLMDiagnoser(grounded, ModeGrounded).Diagnose(context.Background(), b); err != nil {
		t.Fatalf("grounded Diagnose: %v", err)
	}
	if !strings.Contains(grounded.gotUser, "podlog-secretid") {
		t.Error("grounded prompt must contain the evidence ID")
	}
	if !strings.Contains(grounded.gotSystem, "GROUNDING CONSTRAINT") {
		t.Error("grounded system prompt must state the grounding constraint")
	}

	freeform := &mockLLM{model: "m", reply: groundedReply}
	if _, err := NewLLMDiagnoser(freeform, ModeFreeform).Diagnose(context.Background(), b); err != nil {
		t.Fatalf("freeform Diagnose: %v", err)
	}
	if strings.Contains(freeform.gotUser, "podlog-secretid") {
		t.Error("free-form prompt must NOT contain evidence IDs")
	}
	if strings.Contains(freeform.gotSystem, "GROUNDING CONSTRAINT") {
		t.Error("free-form system prompt must not state a grounding constraint")
	}
}

func TestLLMProvenanceRenderedAtC5(t *testing.T) {
	b := bundleWith(ev("podlog-1", platformv1alpha1.EvidenceTypePodLog, "app", "boom", platformv1alpha1.EvidenceRelevanceHigh))
	client := &mockLLM{model: "m", reply: groundedReply}
	d := NewLLMDiagnoser(client, ModeGrounded)
	d.Provenance = &platformv1alpha1.ProvenanceGraph{
		Nodes: []platformv1alpha1.ProvenanceNode{
			{ID: "preview-x", Kind: platformv1alpha1.ProvenanceNodeEntity, Type: "Preview", Label: "x"},
			{ID: "pr-7", Kind: platformv1alpha1.ProvenanceNodeEntity, Type: "PullRequest", Label: "PR #7"},
		},
		Edges: []platformv1alpha1.ProvenanceEdge{{From: "preview-x", To: "pr-7", Relation: "wasDerivedFrom"}},
	}
	if _, err := d.Diagnose(context.Background(), b); err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if !strings.Contains(client.gotUser, "provenance graph") {
		t.Error("C5 prompt must include the provenance graph section")
	}
	if !strings.Contains(client.gotUser, "wasDerivedFrom") {
		t.Error("C5 prompt must render provenance edges")
	}
}
