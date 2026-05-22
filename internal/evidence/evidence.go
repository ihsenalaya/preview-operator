// Package evidence assembles, structures, and grounds failure evidence for
// ephemeral preview environments. It turns the raw signals the operator already
// collects (DiagnosticsStatus, test results, change context, conditions) into a
// typed, addressable, deduplicated FailureEvidenceBundle, and produces an
// auditable FailureReport custom resource from it.
//
// The package performs no Kubernetes API calls of its own: collectors transform
// data already present on the Preview object. This keeps the package pure and
// unit-testable, and keeps evidence collection cheap.
//
// See docs/research/failure-provenance/failure-evidence-model.md for the model.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// idHashLen is the number of hex characters kept from the SHA-256 of an evidence
// item's logical identity. 12 hex chars (48 bits) is ample for per-bundle uniqueness.
const idHashLen = 12

// EvidenceID computes a stable, deterministic identifier for an evidence item from
// its logical identity: the tuple (type, source, resource, content). The same
// logical evidence always yields the same ID, so repeated reconciliation does not
// create duplicate items and a diagnosis can reference an item reliably.
//
// Volatile fields (timestamps, counters) are deliberately excluded from the hash.
func EvidenceID(t platformv1alpha1.EvidenceType, source, resource, content string) string {
	sum := sha256.Sum256([]byte(string(t) + "\x00" + source + "\x00" + resource + "\x00" + content))
	return strings.ToLower(string(t)) + "-" + hex.EncodeToString(sum[:])[:idHashLen]
}

// Bundle is the in-memory failure evidence bundle assembled during reconciliation.
// Items are keyed by ID, so adding the same logical evidence twice is a no-op —
// this is what makes report generation idempotent.
type Bundle struct {
	PreviewName       string
	Namespace         string
	PRNumber          int
	CommitSHA         string
	FailedSuite       string
	FailedTest        string
	FailureDetectedAt metav1.Time

	items     map[string]platformv1alpha1.FailureEvidenceItem
	diagnosis *platformv1alpha1.FailureDiagnosis
}

// NewBundle creates an empty bundle, seeding its identity fields from a Preview.
func NewBundle(c *platformv1alpha1.Preview) *Bundle {
	b := &Bundle{
		items:             map[string]platformv1alpha1.FailureEvidenceItem{},
		FailureDetectedAt: metav1.Now(),
	}
	if c == nil {
		return b
	}
	b.PreviewName = c.Name
	b.Namespace = c.Status.NamespaceName
	b.PRNumber = c.Spec.PRNumber
	if c.Spec.ChangeContext != nil {
		b.CommitSHA = c.Spec.ChangeContext.DiffRef.HeadSHA
	}
	return b
}

// Add inserts an evidence item into the bundle. If the item has no ID, a
// deterministic one is computed. If an item with the same ID is already present,
// Add is a no-op: the bundle never holds duplicate evidence items.
func (b *Bundle) Add(item platformv1alpha1.FailureEvidenceItem) {
	if item.Type == "" {
		return
	}
	if item.ID == "" {
		item.ID = EvidenceID(item.Type, item.Source, item.Resource, item.Message)
	}
	if _, exists := b.items[item.ID]; exists {
		return
	}
	b.items[item.ID] = item
}

// Items returns the evidence items sorted by ID, giving a deterministic order
// across reconciliations.
func (b *Bundle) Items() []platformv1alpha1.FailureEvidenceItem {
	out := make([]platformv1alpha1.FailureEvidenceItem, 0, len(b.items))
	for _, it := range b.items {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Len returns the number of distinct evidence items in the bundle.
func (b *Bundle) Len() int { return len(b.items) }

// Has reports whether an evidence item with the given ID is in the bundle.
func (b *Bundle) Has(id string) bool {
	_, ok := b.items[id]
	return ok
}

// Diagnosis returns the diagnosis attached to the bundle, or nil.
func (b *Bundle) Diagnosis() *platformv1alpha1.FailureDiagnosis { return b.diagnosis }

// SetDiagnosis attaches a diagnosis to the bundle after enforcing the grounding
// constraint: every entry in EvidenceRefs must reference the ID of an evidence
// item that exists in the bundle. If any reference is unknown, the diagnosis is
// rejected and an error listing the unknown IDs is returned — the bundle is left
// unchanged. This is the mechanism evaluated by RQ4 (hallucination control).
func (b *Bundle) SetDiagnosis(d *platformv1alpha1.FailureDiagnosis) error {
	if d == nil {
		b.diagnosis = nil
		return nil
	}
	var unknown []string
	for _, ref := range d.EvidenceRefs {
		if !b.Has(ref) {
			unknown = append(unknown, ref)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("ungrounded diagnosis: references %d unknown evidence ID(s): %s",
			len(unknown), strings.Join(unknown, ", "))
	}
	b.diagnosis = d
	return nil
}

// Summary returns a short human-readable description of the bundle contents.
func (b *Bundle) Summary() string {
	counts := map[platformv1alpha1.EvidenceType]int{}
	for _, it := range b.items {
		counts[it.Type]++
	}
	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, string(t))
	}
	sort.Strings(types)
	parts := make([]string, 0, len(types))
	for _, t := range types {
		parts = append(parts, fmt.Sprintf("%s=%d", t, counts[platformv1alpha1.EvidenceType(t)]))
	}
	if len(parts) == 0 {
		return "no evidence captured"
	}
	return fmt.Sprintf("%d evidence item(s): %s", b.Len(), strings.Join(parts, ", "))
}
