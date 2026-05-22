package evidence

import (
	"fmt"
	"strings"
	"time"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// Collector turns one source of signals already present on a Preview object into
// typed failure evidence items. Collectors perform no Kubernetes API calls.
type Collector interface {
	// Name identifies the collector (used as the evidence Source).
	Name() string
	// Collect extracts evidence items from the Preview.
	Collect(c *platformv1alpha1.Preview) []platformv1alpha1.FailureEvidenceItem
}

// newItem builds a FailureEvidenceItem, redacting secret-bearing content before
// the ID is computed so that redacted and unredacted runs of the same evidence
// still produce the same ID.
func newItem(t platformv1alpha1.EvidenceType, source, resource, content string, rel platformv1alpha1.EvidenceRelevance) platformv1alpha1.FailureEvidenceItem {
	redacted, didRedact := Redact(content)
	return platformv1alpha1.FailureEvidenceItem{
		ID:        EvidenceID(t, source, resource, redacted),
		Type:      t,
		Source:    source,
		Resource:  resource,
		Message:   redacted,
		Relevance: rel,
		Redacted:  didRedact,
	}
}

// DefaultCollectors returns the standard set of Phase-2 collectors.
func DefaultCollectors() []Collector {
	return []Collector{
		ChangeContextCollector{},
		PreviewStatusCollector{},
		EventsCollector{},
		LogsCollector{},
		TestResultCollector{},
	}
}

// AssembleBundle runs the given collectors against a Preview and returns a bundle
// of the de-duplicated evidence items. With no collectors, DefaultCollectors is used.
func AssembleBundle(c *platformv1alpha1.Preview, collectors ...Collector) *Bundle {
	if len(collectors) == 0 {
		collectors = DefaultCollectors()
	}
	b := NewBundle(c)
	if c == nil {
		return b
	}
	for _, col := range collectors {
		for _, item := range col.Collect(c) {
			b.Add(item)
		}
	}
	return b
}

// AssembleBundleForLevel runs the collectors enabled by an evidence level
// (C1..C5), records the level and the assembly duration on the bundle, and
// returns it. This is the entry point used by the operator: the level is the
// EVIDENCE_LEVEL knob, and CollectionDuration is the in-process overhead
// measured by RQ5.
func AssembleBundleForLevel(c *platformv1alpha1.Preview, level Level) *Bundle {
	start := time.Now()
	b := AssembleBundle(c, CollectorsForLevel(level)...)
	b.Level = level
	b.CollectionDuration = time.Since(start)
	return b
}

// ChangeContextCollector turns the PR change context into GitDiff and ChangedFile
// evidence items.
type ChangeContextCollector struct{}

func (ChangeContextCollector) Name() string { return "change-context-collector" }

func (cc ChangeContextCollector) Collect(c *platformv1alpha1.Preview) []platformv1alpha1.FailureEvidenceItem {
	ctx := c.Spec.ChangeContext
	if ctx == nil {
		return nil
	}
	var items []platformv1alpha1.FailureEvidenceItem
	repo := ctx.DiffRef.Repository

	switch {
	case ctx.DiffPatch != "":
		items = append(items, newItem(platformv1alpha1.EvidenceTypeGitDiff,
			cc.Name(), repo, ctx.DiffPatch, platformv1alpha1.EvidenceRelevanceMedium))
	case ctx.DiffPatchRef != "":
		items = append(items, newItem(platformv1alpha1.EvidenceTypeGitDiff,
			cc.Name(), repo,
			fmt.Sprintf("diff stored in ConfigMap %q (key diff.patch)", ctx.DiffPatchRef),
			platformv1alpha1.EvidenceRelevanceMedium))
	}

	for _, f := range ctx.ChangedFiles {
		items = append(items, newItem(platformv1alpha1.EvidenceTypeChangedFile,
			cc.Name(), f.Path, fmt.Sprintf("type=%s", f.Type), changedFileRelevance(f.Type)))
	}
	return items
}

func changedFileRelevance(t platformv1alpha1.ChangeFileType) platformv1alpha1.EvidenceRelevance {
	switch t {
	case platformv1alpha1.ChangeFileTypeDatabaseMigration,
		platformv1alpha1.ChangeFileTypeAPIContract,
		platformv1alpha1.ChangeFileTypeBackend,
		platformv1alpha1.ChangeFileTypeFrontend:
		return platformv1alpha1.EvidenceRelevanceHigh
	default:
		return platformv1alpha1.EvidenceRelevanceLow
	}
}

// PreviewStatusCollector turns Preview status conditions into PreviewCondition
// evidence items.
type PreviewStatusCollector struct{}

func (PreviewStatusCollector) Name() string { return "preview-status-collector" }

func (pc PreviewStatusCollector) Collect(c *platformv1alpha1.Preview) []platformv1alpha1.FailureEvidenceItem {
	var items []platformv1alpha1.FailureEvidenceItem
	for _, cond := range c.Status.Conditions {
		rel := platformv1alpha1.EvidenceRelevanceLow
		if cond.Status != "True" {
			rel = platformv1alpha1.EvidenceRelevanceHigh
		}
		content := fmt.Sprintf("status=%s reason=%s message=%s", cond.Status, cond.Reason, cond.Message)
		item := newItem(platformv1alpha1.EvidenceTypePreviewCondition,
			pc.Name(), cond.Type, content, rel)
		if !cond.LastTransitionTime.IsZero() {
			ts := cond.LastTransitionTime
			item.Timestamp = &ts
		}
		items = append(items, item)
	}
	return items
}

// EventsCollector turns the recent Kubernetes warning events captured in
// DiagnosticsStatus into KubernetesEvent evidence items.
type EventsCollector struct{}

func (EventsCollector) Name() string { return "events-collector" }

func (ec EventsCollector) Collect(c *platformv1alpha1.Preview) []platformv1alpha1.FailureEvidenceItem {
	if c.Status.Diagnostics == nil {
		return nil
	}
	var items []platformv1alpha1.FailureEvidenceItem
	for _, ev := range c.Status.Diagnostics.LastEvents {
		resource := ev
		if idx := strings.Index(ev, ": "); idx > 0 {
			resource = ev[:idx]
		}
		items = append(items, newItem(platformv1alpha1.EvidenceTypeKubernetesEvent,
			ec.Name(), resource, ev, platformv1alpha1.EvidenceRelevanceHigh))
	}
	return items
}

// LogsCollector turns the significant log excerpts and crashed-pod logs captured
// in DiagnosticsStatus into PodLog and JobLog evidence items.
type LogsCollector struct{}

func (LogsCollector) Name() string { return "logs-collector" }

func (lc LogsCollector) Collect(c *platformv1alpha1.Preview) []platformv1alpha1.FailureEvidenceItem {
	if c.Status.Diagnostics == nil {
		return nil
	}
	var items []platformv1alpha1.FailureEvidenceItem
	for _, ex := range c.Status.Diagnostics.SignificantLogs {
		if len(ex.Lines) == 0 {
			continue
		}
		t := platformv1alpha1.EvidenceTypePodLog
		if isJobComponent(ex.Component) {
			t = platformv1alpha1.EvidenceTypeJobLog
		}
		items = append(items, newItem(t, lc.Name(), ex.Source,
			strings.Join(ex.Lines, "\n"), platformv1alpha1.EvidenceRelevanceHigh))
	}
	if logs := c.Status.Diagnostics.PodLogs; len(logs) > 0 {
		items = append(items, newItem(platformv1alpha1.EvidenceTypePodLog,
			lc.Name(), "crashed application pod",
			strings.Join(logs, "\n"), platformv1alpha1.EvidenceRelevanceHigh))
	}
	return items
}

func isJobComponent(component string) bool {
	switch strings.ToLower(component) {
	case "migration", "seed":
		return true
	default:
		return false
	}
}

// TestResultCollector turns the test suite status into TestResult evidence items.
type TestResultCollector struct{}

func (TestResultCollector) Name() string { return "test-result-collector" }

func (tc TestResultCollector) Collect(c *platformv1alpha1.Preview) []platformv1alpha1.FailureEvidenceItem {
	tests := c.Status.Tests
	if tests == nil {
		return nil
	}
	suites := []struct {
		name   string
		result platformv1alpha1.TestResult
	}{
		{"smoke", tests.Smoke},
		{"contract", tests.Contract},
		{"regression", tests.Regression},
		{"e2e", tests.E2E},
		{"migration", tests.Migration},
	}
	var items []platformv1alpha1.FailureEvidenceItem
	for _, s := range suites {
		if s.result.Phase == "" {
			continue
		}
		failed := s.result.Phase == "Failed" || s.result.Failed > 0
		rel := platformv1alpha1.EvidenceRelevanceLow
		if failed {
			rel = platformv1alpha1.EvidenceRelevanceHigh
		}
		content := fmt.Sprintf("phase=%s passed=%d failed=%d", s.result.Phase, s.result.Passed, s.result.Failed)
		if len(s.result.Output) > 0 {
			content += "\n" + strings.Join(s.result.Output, "\n")
		}
		items = append(items, newItem(platformv1alpha1.EvidenceTypeTestResult,
			tc.Name(), s.name, content, rel))
	}
	return items
}
