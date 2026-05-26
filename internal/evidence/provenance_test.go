package evidence

import (
	"testing"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

func TestBuildProvenanceGraph(t *testing.T) {
	b := AssembleBundle(failedPreview())
	known := b.Items()[0].ID
	if err := b.SetDiagnosis(&platformv1alpha1.FailureDiagnosis{
		ProbableCause: "invalid SQL in migration",
		Component:     "migration-job",
		Category:      platformv1alpha1.FailureCategoryDatabase,
		Confidence:    "high",
		EvidenceRefs:  []string{known},
	}); err != nil {
		t.Fatalf("SetDiagnosis: %v", err)
	}

	g := BuildProvenanceGraph(b)
	if g == nil {
		t.Fatal("BuildProvenanceGraph returned nil")
	}

	nodes := map[string]platformv1alpha1.ProvenanceNode{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}

	// The change-context spine, the preview, the diagnosis, and the operator agent
	// must all be present.
	for _, want := range []string{"pr-42", "commit-deadbeefcafe", "preview-pr-42", "diagnosis", "agent-operator"} {
		if _, ok := nodes[want]; !ok {
			t.Errorf("provenance graph is missing node %q", want)
		}
	}

	// Every evidence item must be a node in the graph.
	for _, it := range b.Items() {
		if _, ok := nodes[it.ID]; !ok {
			t.Errorf("evidence item %q is not represented as a graph node", it.ID)
		}
	}

	// The grounding edge must exist: evidence --supports--> diagnosis.
	grounded := false
	for _, e := range g.Edges {
		if e.From == known && e.To == "diagnosis" && e.Relation == relSupports {
			grounded = true
		}
	}
	if !grounded {
		t.Error("missing grounding edge: evidence --supports--> diagnosis")
	}

	// Construction must be deterministic.
	g2 := BuildProvenanceGraph(b)
	if len(g2.Nodes) != len(g.Nodes) || len(g2.Edges) != len(g.Edges) {
		t.Errorf("BuildProvenanceGraph not deterministic: nodes %d/%d edges %d/%d",
			len(g.Nodes), len(g2.Nodes), len(g.Edges), len(g2.Edges))
	}
}

func TestBuildProvenanceGraphNilBundle(t *testing.T) {
	if g := BuildProvenanceGraph(nil); g != nil {
		t.Error("BuildProvenanceGraph(nil) must return nil")
	}
}
