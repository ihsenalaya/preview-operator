package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/evidence"
)

// maxMessageChars caps how much of one evidence item's message is rendered into
// a prompt, so a large log excerpt cannot crowd out the rest of the bundle.
const maxMessageChars = 1500

// LLMClient is the minimal interface the LLM diagnoser needs. Implementations
// must request temperature 0 for reproducibility — see OpenAIClient.
type LLMClient interface {
	// Complete sends a system prompt and a user prompt and returns the raw
	// model text.
	Complete(ctx context.Context, system, user string) (string, error)
	// Model returns the model identifier, recorded on the Result so a run is
	// reproducible.
	Model() string
}

// LLMDiagnoser is the LLM-backed diagnostic engine. It renders the evidence
// bundle into a prompt, asks the model for a structured diagnosis, and — in
// grounded mode — strips any evidence reference the model invented.
//
// The grounded vs free-form distinction is the RQ4 ablation: grounded mode
// shows the model the evidence IDs and demands that every claim cite one;
// free-form mode shows the model only the raw signals, with no IDs and no
// grounding instruction.
type LLMDiagnoser struct {
	// Client talks to the model endpoint.
	Client LLMClient
	// Mode selects grounded or free-form diagnosis.
	Mode Mode
	// Provenance, when set, is rendered into the grounded prompt. The harness
	// supplies it only for configuration C5 (provenance graph + full bundle),
	// which is what makes C5 a distinct configuration from C4 for the LLM
	// engine. It is ignored in free-form mode.
	Provenance *platformv1alpha1.ProvenanceGraph
}

// NewLLMDiagnoser returns an LLM-backed Diagnoser.
func NewLLMDiagnoser(client LLMClient, mode Mode) *LLMDiagnoser {
	return &LLMDiagnoser{Client: client, Mode: mode}
}

// Diagnose renders the bundle into a prompt, calls the model, parses the JSON
// response, and applies the grounding policy for the configured mode.
func (d *LLMDiagnoser) Diagnose(ctx context.Context, b *evidence.Bundle) (*Result, error) {
	if d.Client == nil {
		return nil, fmt.Errorf("diagnosis: LLM client is nil")
	}
	mode := d.Mode
	if mode == "" {
		mode = ModeGrounded
	}

	system := systemPrompt(mode)
	user := d.userPrompt(b, mode)

	raw, err := d.Client.Complete(ctx, system, user)
	if err != nil {
		return nil, fmt.Errorf("diagnosis: LLM call failed: %w", err)
	}

	diag, err := parseDiagnosis(raw)
	if err != nil {
		return nil, fmt.Errorf("diagnosis: %w", err)
	}

	res := &Result{
		Engine:        EngineLLM,
		Mode:          mode,
		Model:         d.Client.Model(),
		EvidenceLevel: string(b.Level),
		Diagnosis:     diag,
		RawOutput:     raw,
	}
	res.HallucinatedRefs = applyGrounding(diag, b, mode)
	return res, nil
}

// systemPrompt returns the system prompt for a mode. The two prompts differ
// only in the grounding instruction and the JSON shape — this is the controlled
// difference the RQ4 ablation measures.
func systemPrompt(mode Mode) string {
	const base = `You are a root-cause analysis assistant for Kubernetes pull-request preview environments.
You are given the evidence captured from one failed preview environment.
Diagnose the single most probable root cause.

The "category" field must be exactly one of:
database, configuration, infrastructure, application, observability, test-reliability, unknown.
The "confidence" field must be exactly one of: low, medium, high.
If the evidence does not support a confident diagnosis, set category to "unknown" and confidence to "low".`

	if mode == ModeGrounded {
		return base + `

Each evidence item is prefixed with a stable ID in square brackets, e.g. [podlog-1a2b3c].

GROUNDING CONSTRAINT: every ID you place in "evidenceRefs" MUST be the ID of an
evidence item shown below. Do not invent IDs. Cite only the items you actually
used to reach the diagnosis.

Respond ONLY with a JSON object of exactly this shape:
{
  "probableCause": "one or two sentences",
  "component": "the component most likely responsible",
  "category": "<category>",
  "confidence": "<confidence>",
  "evidenceRefs": ["<evidence id>", "..."],
  "recommendations": ["actionable step", "..."]
}`
	}

	return base + `

Respond ONLY with a JSON object of exactly this shape:
{
  "probableCause": "one or two sentences",
  "component": "the component most likely responsible",
  "category": "<category>",
  "confidence": "<confidence>",
  "recommendations": ["actionable step", "..."]
}`
}

// userPrompt renders the bundle. In grounded mode every item carries its ID; in
// free-form mode the IDs are withheld so the model has nothing to cite.
func (d *LLMDiagnoser) userPrompt(b *evidence.Bundle, mode Mode) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Failed preview environment\n")
	if b.PreviewName != "" {
		fmt.Fprintf(&sb, "preview: %s\n", b.PreviewName)
	}
	if b.PRNumber != 0 {
		fmt.Fprintf(&sb, "pull request: #%d\n", b.PRNumber)
	}
	if b.FailedSuite != "" {
		fmt.Fprintf(&sb, "failed suite: %s\n", b.FailedSuite)
	}
	if b.FailedTest != "" {
		fmt.Fprintf(&sb, "failed test: %s\n", b.FailedTest)
	}

	items := b.Items()
	if len(items) == 0 {
		sb.WriteString("\n(no evidence was captured)\n")
		return sb.String()
	}

	sb.WriteString("\nEvidence:\n")
	for _, it := range items {
		if mode == ModeGrounded {
			fmt.Fprintf(&sb, "\n[%s] type=%s resource=%s relevance=%s\n",
				it.ID, it.Type, it.Resource, it.Relevance)
		} else {
			fmt.Fprintf(&sb, "\n%s (%s)\n", it.Type, it.Resource)
		}
		sb.WriteString(indentBlock(truncate(it.Message, maxMessageChars)))
		sb.WriteString("\n")
	}

	if mode == ModeGrounded && d.Provenance != nil && len(d.Provenance.Edges) > 0 {
		sb.WriteString("\nFailure provenance graph (PR change context -> resources -> evidence):\n")
		sb.WriteString(renderProvenance(d.Provenance))
	}

	return sb.String()
}

// renderProvenance renders the provenance graph as a deterministic edge list.
func renderProvenance(g *platformv1alpha1.ProvenanceGraph) string {
	labels := make(map[string]string, len(g.Nodes))
	for _, n := range g.Nodes {
		label := n.Label
		if label == "" {
			label = n.ID
		}
		labels[n.ID] = fmt.Sprintf("%s(%s)", n.Type, label)
	}
	lines := make([]string, 0, len(g.Edges))
	for _, e := range g.Edges {
		from, to := labels[e.From], labels[e.To]
		if from == "" {
			from = e.From
		}
		if to == "" {
			to = e.To
		}
		lines = append(lines, fmt.Sprintf("  %s --%s--> %s", from, e.Relation, to))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// parseDiagnosis extracts a FailureDiagnosis from raw model text. It tolerates
// markdown code fences and leading/trailing prose by isolating the outermost
// JSON object before unmarshalling.
func parseDiagnosis(raw string) (*platformv1alpha1.FailureDiagnosis, error) {
	body := extractJSONObject(raw)
	if body == "" {
		return nil, fmt.Errorf("no JSON object found in model output")
	}
	var parsed struct {
		ProbableCause   string   `json:"probableCause"`
		Component       string   `json:"component"`
		Category        string   `json:"category"`
		Confidence      string   `json:"confidence"`
		EvidenceRefs    []string `json:"evidenceRefs"`
		Recommendations []string `json:"recommendations"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, fmt.Errorf("invalid JSON in model output: %w", err)
	}
	return &platformv1alpha1.FailureDiagnosis{
		ProbableCause:   strings.TrimSpace(parsed.ProbableCause),
		Component:       strings.TrimSpace(parsed.Component),
		Category:        normaliseCategory(parsed.Category),
		Confidence:      normaliseConfidence(parsed.Confidence),
		EvidenceRefs:    dedupe(parsed.EvidenceRefs),
		Recommendations: parsed.Recommendations,
	}, nil
}

// extractJSONObject returns the substring from the first '{' to the matching
// final '}'. It strips markdown fences first.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// normaliseCategory maps free model text onto the closed FailureCategory
// vocabulary, defaulting to "unknown" rather than failing.
func normaliseCategory(s string) platformv1alpha1.FailureCategory {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "database":
		return platformv1alpha1.FailureCategoryDatabase
	case "configuration", "config":
		return platformv1alpha1.FailureCategoryConfiguration
	case "infrastructure", "infra":
		return platformv1alpha1.FailureCategoryInfrastructure
	case "application", "app":
		return platformv1alpha1.FailureCategoryApplication
	case "observability":
		return platformv1alpha1.FailureCategoryObservability
	case "test-reliability", "test", "flaky":
		return platformv1alpha1.FailureCategoryTestReliability
	default:
		return platformv1alpha1.FailureCategoryUnknown
	}
}

// normaliseConfidence maps free model text onto the low|medium|high vocabulary.
func normaliseConfidence(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return "high"
	case "medium", "med", "moderate":
		return "medium"
	case "low":
		return "low"
	default:
		return ""
	}
}

// indentBlock indents every line of s by two spaces for prompt readability.
func indentBlock(s string) string {
	if s == "" {
		return "  (empty)"
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

// truncate caps s at max characters, appending an explicit marker.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... (truncated)"
}
