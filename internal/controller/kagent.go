package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// a2aMessage is the JSON-RPC 2.0 envelope sent to a kagent agent.
type a2aMessage struct {
	JSONRPC string     `json:"jsonrpc"`
	Method  string     `json:"method"`
	ID      string     `json:"id"`
	Params  a2aParams  `json:"params"`
}

type a2aParams struct {
	Message a2aUserMessage `json:"message"`
}

type a2aUserMessage struct {
	Role      string     `json:"role"`
	MessageID string     `json:"messageId"`
	Parts     []a2aPart  `json:"parts"`
}

type a2aPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// a2aResponse is the JSON-RPC 2.0 response from a kagent agent.
type a2aResponse struct {
	Result *a2aResult `json:"result"`
	Error  *a2aError  `json:"error"`
}

type a2aResult struct {
	Status    *a2aStatus     `json:"status"`
	Artifacts []a2aArtifact  `json:"artifacts"`
}

type a2aArtifact struct {
	Parts []a2aAgentPart `json:"parts"`
}

type a2aStatus struct {
	State   string     `json:"state"`
	Message *a2aAgentMessage `json:"message"`
}

type a2aAgentMessage struct {
	Parts []a2aAgentPart `json:"parts"`
}

type a2aAgentPart struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type a2aError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func kagentEnabled(c *platformv1alpha1.Preview) bool {
	return c.Spec.Kagent != nil && c.Spec.Kagent.Enabled
}

func kagentAgentURL(c *platformv1alpha1.Preview) string {
	ns := "kagent-system"
	name := "preview-troubleshooter-agent"
	if c.Spec.Kagent != nil {
		if c.Spec.Kagent.Namespace != "" {
			ns = c.Spec.Kagent.Namespace
		}
		if c.Spec.Kagent.AgentName != "" {
			name = c.Spec.Kagent.AgentName
		}
	}
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", name, ns)
}

func kagentDiffAnalyzerURL(c *platformv1alpha1.Preview) string {
	ns := "kagent-system"
	name := "preview-diff-analyzer"
	if c.Spec.Kagent != nil {
		if c.Spec.Kagent.Namespace != "" {
			ns = c.Spec.Kagent.Namespace
		}
		if c.Spec.Kagent.DiffAnalyzerAgentName != "" {
			name = c.Spec.Kagent.DiffAnalyzerAgentName
		}
	}
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", name, ns)
}

// triggerKagentDiffAnalysis calls the preview-diff-analyzer agent when the preview
// first becomes Running. It is idempotent: fires once per preview lifecycle.
// Must be called in a goroutine with context.Background() — the reconcile context
// is cancelled as soon as the reconcile function returns.
func (r *PreviewReconciler) triggerKagentDiffAnalysis(ctx context.Context, previewName string, prNumber int, owner, repo, agentURL string) {
	logger := log.FromContext(ctx).WithValues("preview", previewName)
	logger.Info("diff-analyzer: starting", "pr", prNumber, "repo", repo)

	// Fetch a fresh copy to avoid stale ResourceVersion.
	var c platformv1alpha1.Preview
	if err := r.Get(ctx, types.NamespacedName{Name: previewName}, &c); err != nil {
		logger.Error(err, "diff-analyzer: failed to fetch preview")
		return
	}

	// Idempotency: only run once (Succeeded) or skip if already running.
	if c.Status.DiffAnalysis != nil {
		switch c.Status.DiffAnalysis.Phase {
		case phaseSucceeded, "Running":
			logger.Info("diff-analyzer: already done or running, skipping")
			return
		case phaseFailed:
			if c.Status.DiffAnalysis.TriggeredAt != nil &&
				time.Since(c.Status.DiffAnalysis.TriggeredAt.Time) < 5*time.Minute {
				logger.Info("diff-analyzer: failed recently, backing off")
				return
			}
		}
	}

	now := metav1.Now()
	c.Status.DiffAnalysis = &platformv1alpha1.KagentStatus{Phase: "Running", TriggeredAt: &now}
	if err := r.Status().Update(ctx, &c); err != nil {
		logger.Error(err, "diff-analyzer: failed to set Running status")
		return
	}

	prompt := fmt.Sprintf(
		"Analyze the diff for PR #%d in repo %s/%s and post a structured analysis comment on the PR.",
		prNumber, owner, repo,
	)
	payload := a2aMessage{
		JSONRPC: "2.0",
		Method:  "message/send",
		ID:      uuid.New().String(),
		Params: a2aParams{
			Message: a2aUserMessage{
				Role:      "user",
				MessageID: uuid.New().String(),
				Parts:     []a2aPart{{Type: "text", Text: prompt}},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error(err, "diff-analyzer: marshal payload")
		r.setDiffAnalysisPhase(ctx, previewName, phaseFailed)
		return
	}

	httpCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, http.MethodPost, agentURL, bytes.NewReader(body))
	if err != nil {
		logger.Error(err, "diff-analyzer: create request")
		r.setDiffAnalysisPhase(ctx, previewName, phaseFailed)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Error(err, "diff-analyzer: A2A call failed", "url", agentURL)
		r.setDiffAnalysisPhase(ctx, previewName, phaseFailed)
		return
	}
	defer resp.Body.Close()

	r.setDiffAnalysisPhase(ctx, previewName, phaseSucceeded)
	logger.Info("diff-analyzer: succeeded", "pr", prNumber, "repo", repo)
}

func (r *PreviewReconciler) setDiffAnalysisPhase(ctx context.Context, previewName, phase string) {
	var c platformv1alpha1.Preview
	if err := r.Get(ctx, types.NamespacedName{Name: previewName}, &c); err != nil {
		return
	}
	if c.Status.DiffAnalysis == nil {
		c.Status.DiffAnalysis = &platformv1alpha1.KagentStatus{}
	}
	c.Status.DiffAnalysis.Phase = phase
	_ = r.Status().Update(ctx, &c)
}

// triggerKagentAnalysis calls the preview-troubleshooter-agent via the A2A
// JSON-RPC API, waits for the analysis, and posts (or updates) a GitHub PR comment.
// It fires once per test run: when tests.phase=Failed and kagent.phase≠Succeeded.
func (r *PreviewReconciler) triggerKagentAnalysis(ctx context.Context, c *platformv1alpha1.Preview) {
	logger := log.FromContext(ctx)

	if !kagentEnabled(c) {
		return
	}
	if c.Status.Tests == nil || c.Status.Tests.Phase != phaseFailed {
		return
	}
	// Idempotency guard — already completed for this test run.
	if c.Status.Kagent != nil && c.Status.Kagent.Phase == phaseSucceeded {
		return
	}
	// Don't retry while already running or within 5 minutes of last attempt.
	if c.Status.Kagent != nil {
		if c.Status.Kagent.Phase == "Running" {
			return
		}
		if c.Status.Kagent.Phase == phaseFailed && c.Status.Kagent.TriggeredAt != nil {
			if time.Since(c.Status.Kagent.TriggeredAt.Time) < 5*time.Minute {
				return
			}
		}
	}

	r.setKagentPhase(ctx, c, "Running")

	analysis, err := r.callKagentAgent(ctx, c)
	if err != nil {
		logger.Error(err, "kagent analysis failed", "preview", c.Name)
		r.setKagentPhase(ctx, c, phaseFailed)
		return
	}

	commentID, err := r.postKagentComment(ctx, c, analysis)
	if err != nil {
		logger.Error(err, "Failed to post kagent comment to GitHub", "preview", c.Name)
		r.setKagentPhase(ctx, c, phaseFailed)
		return
	}

	if c.Status.Kagent == nil {
		c.Status.Kagent = &platformv1alpha1.KagentStatus{}
	}
	c.Status.Kagent.Phase = phaseSucceeded
	c.Status.Kagent.CommentID = commentID
	_ = r.Status().Update(ctx, c)
	logger.Info("kagent analysis posted to GitHub", "commentId", commentID)
}

// callKagentAgent sends a message to the agent via A2A JSON-RPC and returns
// the text of the agent's response.
func (r *PreviewReconciler) callKagentAgent(ctx context.Context, c *platformv1alpha1.Preview) (string, error) {
	agentURL := kagentAgentURL(c)

	payload := a2aMessage{
		JSONRPC: "2.0",
		Method:  "message/send",
		ID:      uuid.New().String(),
		Params: a2aParams{
			Message: a2aUserMessage{
				Role:      "user",
				MessageID: uuid.New().String(),
				Parts:     []a2aPart{{Type: "text", Text: buildAnalysisPrompt(c)}},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal A2A payload: %w", err)
	}

	// Allow up to 3 minutes for the LLM to complete.
	httpCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, http.MethodPost, agentURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("A2A call: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read A2A response: %w", err)
	}

	var a2aResp a2aResponse
	if err := json.Unmarshal(rawBody, &a2aResp); err != nil {
		return "", fmt.Errorf("unmarshal A2A response: %w", err)
	}

	if a2aResp.Error != nil {
		return "", fmt.Errorf("A2A error %d: %s", a2aResp.Error.Code, a2aResp.Error.Message)
	}
	if a2aResp.Result == nil || a2aResp.Result.Status == nil {
		return "", fmt.Errorf("empty A2A result")
	}
	if a2aResp.Result.Status.State == "failed" {
		return "", fmt.Errorf("agent returned failed state")
	}

	// Collect text from artifacts first (kagent v0.9+), then fall back to status.message.
	var texts []string
	for _, artifact := range a2aResp.Result.Artifacts {
		for _, part := range artifact.Parts {
			if part.Kind == "text" && part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
	}
	if len(texts) == 0 && a2aResp.Result.Status.Message != nil {
		for _, part := range a2aResp.Result.Status.Message.Parts {
			if part.Kind == "text" && part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
	}
	if len(texts) == 0 {
		return "", fmt.Errorf("agent returned empty response")
	}
	return strings.Join(texts, "\n"), nil
}

func buildAnalysisPrompt(c *platformv1alpha1.Preview) string {
	tests := c.Status.Tests

	var b strings.Builder
	fmt.Fprintf(&b, "Analyze the test failure for preview environment %q.\n\n", c.Name)
	fmt.Fprintf(&b, "Context:\n")
	fmt.Fprintf(&b, "  PR #%d — branch: %s\n", c.Spec.PRNumber, c.Spec.Branch)
	fmt.Fprintf(&b, "  Namespace: %s\n", c.Status.NamespaceName)
	if c.Spec.GitHub != nil {
		fmt.Fprintf(&b, "  GitHub repo: %s/%s\n", c.Spec.GitHub.Owner, c.Spec.GitHub.Repo)
	}

	// Include test output directly to reduce the number of k8s tool calls needed.
	if tests != nil {
		b.WriteString("\nTest results:\n")
		appendSuiteOutput(&b, "smoke", tests.Smoke)
		appendSuiteOutput(&b, "microcks-contract", tests.Contract)
		appendSuiteOutput(&b, "regression", tests.Regression)
		appendSuiteOutput(&b, "e2e", tests.E2E)
	}

	b.WriteString("\nUsing this information and any pod/job events you can gather from the namespace, ")
	b.WriteString("produce a structured failure analysis in the format described in your system prompt. ")
	b.WriteString("Focus on the failed suites only — skip tool calls for passing suites.")
	return b.String()
}

func appendSuiteOutput(b *strings.Builder, name string, suite platformv1alpha1.TestResult) {
	if suite.Phase == "" {
		return
	}
	fmt.Fprintf(b, "  %s: %s", name, suite.Phase)
	if suite.Passed > 0 || suite.Failed > 0 {
		fmt.Fprintf(b, " (%dp/%df)", suite.Passed, suite.Failed)
	}
	b.WriteString("\n")
	// Include up to 10 output lines to stay within context limits.
	limit := 10
	for i, line := range suite.Output {
		if i >= limit {
			fmt.Fprintf(b, "    ... (%d more lines)\n", len(suite.Output)-limit)
			break
		}
		fmt.Fprintf(b, "    %s\n", line)
	}
}

func (r *PreviewReconciler) setKagentPhase(ctx context.Context, c *platformv1alpha1.Preview, phase string) {
	if c.Status.Kagent == nil {
		c.Status.Kagent = &platformv1alpha1.KagentStatus{}
	}
	c.Status.Kagent.Phase = phase
	if phase == "Running" {
		now := metav1.Now()
		c.Status.Kagent.TriggeredAt = &now
	}
	_ = r.Status().Update(ctx, c)
}
