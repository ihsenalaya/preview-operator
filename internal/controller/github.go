package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
)

const (
	defaultGitHubAPIBaseURL      = "https://api.github.com"
	defaultGitHubTokenSecretKey  = "token"
	defaultGitHubSecretNamespace = "cellenza-operator-system"
)

type githubDeploymentStatusRequest struct {
	State          string `json:"state"`
	EnvironmentURL string `json:"environment_url,omitempty"`
	Description    string `json:"description,omitempty"`
	AutoInactive   bool   `json:"auto_inactive"`
}

type githubIssueCommentRequest struct {
	Body string `json:"body"`
}

type githubIssueCommentResponse struct {
	ID int64 `json:"id"`
}

func (r *CellenzaReconciler) syncGitHub(ctx context.Context, c *platformv1alpha1.Cellenza, state, environmentURL, description string, commentOnReady bool) {
	logger := log.FromContext(ctx)
	if !githubEnabled(c) {
		return
	}

	if githubAlreadyNotified(c, state, environmentURL, commentOnReady) {
		return
	}

	token, err := r.githubToken(ctx, c)
	if err != nil {
		r.recordGitHubError(ctx, c, err)
		logger.Error(err, "Failed to read GitHub token", "name", c.Name)
		return
	}

	if err := r.createGitHubDeploymentStatus(ctx, c, token, state, environmentURL, description); err != nil {
		r.recordGitHubError(ctx, c, err)
		logger.Error(err, "Failed to update GitHub Deployment", "name", c.Name)
		return
	}

	if err := r.postGitHubPhaseComment(ctx, c, token); err != nil {
		r.recordGitHubError(ctx, c, err)
		logger.Error(err, "Failed to post phase comment", "name", c.Name)
		return
	}

	var commentID int64
	if commentOnReady {
		var err error
		commentID, err = r.createGitHubReadyComment(ctx, c, token, environmentURL)
		if err != nil {
			r.recordGitHubError(ctx, c, err)
			logger.Error(err, "Failed to comment on GitHub pull request", "name", c.Name)
			return
		}
	}

	r.recordGitHubSuccess(ctx, c, state, environmentURL, commentID)
}

func (r *CellenzaReconciler) postGitHubPhaseComment(ctx context.Context, c *platformv1alpha1.Cellenza, token string) error {
	spec := c.Spec.GitHub
	if spec == nil || spec.Owner == "" || spec.Repo == "" {
		return nil
	}

	var body string
	switch c.Status.Phase {
	case platformv1alpha1.PhaseProvisioning:
		body = fmt.Sprintf("**Cellenza Preview Provisioning**\n\nEnvironment: `%s`\n\nCreating namespace, PostgreSQL, database tasks, and Kubernetes resources.", githubEnvironment(c))
	case platformv1alpha1.PhaseFailed:
		body = githubFailedCommentBody(c)
	default:
		return nil
	}

	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments",
		url.PathEscape(spec.Owner),
		url.PathEscape(spec.Repo),
		c.Spec.PRNumber,
	)
	return r.githubPost(ctx, token, path, githubIssueCommentRequest{Body: body}, nil)
}

func githubEnabled(c *platformv1alpha1.Cellenza) bool {
	return c.Spec.GitHub != nil && c.Spec.GitHub.Enabled
}

func githubAlreadyNotified(c *platformv1alpha1.Cellenza, state, environmentURL string, commentOnReady bool) bool {
	if c.Status.GitHub == nil {
		return false
	}
	if c.Status.GitHub.DeploymentState != state {
		return false
	}
	if c.Status.GitHub.LastEnvironmentURL != environmentURL {
		return false
	}
	if c.Status.GitHub.LastNotifiedPhase != c.Status.Phase {
		return false
	}
	return !commentOnReady || c.Status.GitHub.CommentID != 0
}

func (r *CellenzaReconciler) githubToken(ctx context.Context, c *platformv1alpha1.Cellenza) (string, error) {
	ref := c.Spec.GitHub.TokenSecretRef
	if ref == nil || ref.Name == "" {
		return "", fmt.Errorf("spec.github.tokenSecretRef.name is required when GitHub integration is enabled")
	}

	namespace := ref.Namespace
	if namespace == "" {
		namespace = defaultGitHubSecretNamespace
	}
	key := ref.Key
	if key == "" {
		key = defaultGitHubTokenSecretKey
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
		if errors.IsNotFound(err) {
			return "", fmt.Errorf("GitHub token Secret %s/%s was not found", namespace, ref.Name)
		}
		return "", err
	}

	tokenBytes, ok := secret.Data[key]
	if !ok || len(tokenBytes) == 0 {
		return "", fmt.Errorf("GitHub token Secret %s/%s does not contain key %q", namespace, ref.Name, key)
	}
	return strings.TrimSpace(string(tokenBytes)), nil
}

func (r *CellenzaReconciler) createGitHubDeploymentStatus(ctx context.Context, c *platformv1alpha1.Cellenza, token, state, environmentURL, description string) error {
	spec := c.Spec.GitHub
	if spec.Owner == "" || spec.Repo == "" {
		return fmt.Errorf("spec.github.owner and spec.github.repo are required")
	}
	if spec.DeploymentID == 0 {
		return fmt.Errorf("spec.github.deploymentId is required")
	}

	payload := githubDeploymentStatusRequest{
		State:          state,
		EnvironmentURL: environmentURL,
		Description:    description,
		AutoInactive:   false,
	}

	path := fmt.Sprintf("/repos/%s/%s/deployments/%d/statuses",
		url.PathEscape(spec.Owner),
		url.PathEscape(spec.Repo),
		spec.DeploymentID,
	)
	return r.githubPost(ctx, token, path, payload, nil)
}

func (r *CellenzaReconciler) createGitHubReadyComment(ctx context.Context, c *platformv1alpha1.Cellenza, token, environmentURL string) (int64, error) {
	spec := c.Spec.GitHub
	if spec.Owner == "" || spec.Repo == "" {
		return 0, fmt.Errorf("spec.github.owner and spec.github.repo are required")
	}

	body := githubReadyCommentBody(c, environmentURL)
	payload := githubIssueCommentRequest{Body: body}
	var response githubIssueCommentResponse
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments",
		url.PathEscape(spec.Owner),
		url.PathEscape(spec.Repo),
		c.Spec.PRNumber,
	)
	if err := r.githubPost(ctx, token, path, payload, &response); err != nil {
		return 0, err
	}
	if response.ID == 0 {
		return -1, nil
	}
	return response.ID, nil
}

func githubReadyCommentBody(c *platformv1alpha1.Cellenza, environmentURL string) string {
	var b strings.Builder
	b.WriteString("## Cellenza Preview Ready\n\n")
	b.WriteString(fmt.Sprintf("**URL:** %s\n\n", environmentURL))
	b.WriteString(fmt.Sprintf("Environment: `%s`\n", githubEnvironment(c)))
	b.WriteString(fmt.Sprintf("Namespace: `%s`\n", c.Status.NamespaceName))
	if c.Status.ExpiresAt != nil {
		b.WriteString(fmt.Sprintf("Expires at: `%s`\n", c.Status.ExpiresAt.Format(time.RFC3339)))
	}

	b.WriteString("\n### Evidence\n\n")
	b.WriteString("- App: ready\n")
	if databaseEnabled(c) && c.Status.Database != nil {
		b.WriteString(fmt.Sprintf("- PostgreSQL: %s\n", readyLabel(c.Status.Database.Ready)))
		b.WriteString(fmt.Sprintf("- Migration: %s\n", defaultStatus(c.Status.Database.Migration)))
		b.WriteString(fmt.Sprintf("- Seed: %s\n", defaultStatus(c.Status.Database.Seed)))
	} else {
		b.WriteString("- PostgreSQL: disabled\n")
	}
	if c.Spec.Telemetry != nil && c.Spec.Telemetry.Enabled {
		b.WriteString("- Telemetry: enabled\n")
	} else {
		b.WriteString("- Telemetry: disabled\n")
	}

	b.WriteString("\n### AI-Assisted Summary\n\n")
	b.WriteString("The preview is healthy: the operator observed the app deployment as ready")
	if databaseEnabled(c) && c.Status.Database != nil {
		b.WriteString(", PostgreSQL is available")
		if c.Status.Database.Migration != "" {
			b.WriteString(fmt.Sprintf(", migration is `%s`", c.Status.Database.Migration))
		}
		if c.Status.Database.Seed != "" {
			b.WriteString(fmt.Sprintf(", seed is `%s`", c.Status.Database.Seed))
		}
	}
	b.WriteString(".\n")

	b.WriteString(buildAIEnrichmentSection(c))
	b.WriteString("\nManaged by [Cellenza Operator](https://github.com/ihsenalaya/cellenza-operator)")
	return b.String()
}

func buildAIEnrichmentSection(c *platformv1alpha1.Cellenza) string {
	aiStatus := c.Status.AIEnrichment
	if aiStatus == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n### AI Enrichment\n\n")
	if c.Spec.AIEnrichment != nil && c.Spec.AIEnrichment.Seed != nil && c.Spec.AIEnrichment.Seed.Enabled {
		b.WriteString(fmt.Sprintf("- Seed: %s `%s`\n", statusIcon(aiStatus.SeedStatus), defaultStatus(aiStatus.SeedStatus)))
	}
	if c.Spec.AIEnrichment != nil && c.Spec.AIEnrichment.Tests != nil && c.Spec.AIEnrichment.Tests.Enabled {
		b.WriteString(fmt.Sprintf("- Tests: %s `%s`\n", statusIcon(aiStatus.TestsStatus), defaultStatus(aiStatus.TestsStatus)))
		for _, line := range aiStatus.TestResults {
			b.WriteString(fmt.Sprintf("  - `%s`\n", line))
		}
	}
	if aiStatus.Error != "" {
		b.WriteString(fmt.Sprintf("\n> Warning: %s\n", aiStatus.Error))
		b.WriteString("> Relancer avec `@cellenza enrich pr-N`\n")
	}
	return b.String()
}

func statusIcon(status string) string {
	switch status {
	case phaseSucceeded:
		return "SUCCESS"
	case phaseFailed:
		return "FAIL"
	case phaseRunning, phaseGenerating:
		return "RUN"
	default:
		return "SKIP"
	}
}

func githubFailedCommentBody(c *platformv1alpha1.Cellenza) string {
	var b strings.Builder
	b.WriteString("## Cellenza Preview Failed\n\n")
	b.WriteString(fmt.Sprintf("Environment: `%s`\n", githubEnvironment(c)))
	if c.Status.NamespaceName != "" {
		b.WriteString(fmt.Sprintf("Namespace: `%s`\n", c.Status.NamespaceName))
	}

	if c.Status.Diagnostics != nil {
		diag := c.Status.Diagnostics
		b.WriteString("\n### Diagnosis\n\n")
		b.WriteString(fmt.Sprintf("- Reason: `%s`\n", defaultStatus(diag.Reason)))
		b.WriteString(fmt.Sprintf("- Component: `%s`\n", defaultStatus(diag.Component)))
		if diag.Message != "" {
			b.WriteString(fmt.Sprintf("- Message: %s\n", diag.Message))
		}
		if diag.RootCause != "" {
			b.WriteString(fmt.Sprintf("- Probable cause: **%s**\n", diag.RootCause))
		}
		if diag.Confidence != "" {
			b.WriteString(fmt.Sprintf("- Confidence: `%s`\n", diag.Confidence))
		}
		if len(diag.SignificantLogs) > 0 {
			b.WriteString("\n### Significant Logs\n\n")
			for _, excerpt := range diag.SignificantLogs {
				if len(excerpt.Lines) == 0 {
					continue
				}
				b.WriteString(fmt.Sprintf("**%s**", defaultStatus(excerpt.Component)))
				if excerpt.Source != "" {
					b.WriteString(fmt.Sprintf(" — `%s`", excerpt.Source))
				}
				b.WriteString("\n\n```text\n")
				for _, line := range excerpt.Lines {
					b.WriteString(line)
					b.WriteByte('\n')
				}
				b.WriteString("```\n\n")
			}
		}
		if len(diag.Recommendations) > 0 {
			b.WriteString("\n### Recommendations\n\n")
			for i, recommendation := range diag.Recommendations {
				b.WriteString(fmt.Sprintf("%d. %s\n", i+1, recommendation))
			}
		}
		if len(diag.LastEvents) > 0 {
			b.WriteString("\n### Recent Warning Events\n\n")
			for _, event := range diag.LastEvents {
				b.WriteString(fmt.Sprintf("- %s\n", event))
			}
		}
		if len(diag.DebugCommands) > 0 {
			b.WriteString("\n### Debug Commands\n\n```bash\n")
			for _, command := range diag.DebugCommands {
				b.WriteString(command)
				b.WriteByte('\n')
			}
			b.WriteString("```\n")
		}
	} else {
		b.WriteString(fmt.Sprintf("\nReason: `%s`\n", githubDescriptionForPhase(c)))
		b.WriteString(fmt.Sprintf("\nRun `kubectl describe cellenza %s` for details.\n", c.Name))
	}

	return b.String()
}

func readyLabel(ready bool) string {
	if ready {
		return "ready"
	}
	return "not ready"
}

func defaultStatus(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func (r *CellenzaReconciler) githubPost(ctx context.Context, token, path string, payload any, response any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	baseURL := r.GitHubAPIBaseURL
	if baseURL == "" {
		baseURL = defaultGitHubAPIBaseURL
	}
	endpoint := strings.TrimRight(baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := r.GitHubHTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if readErr != nil {
			return fmt.Errorf("GitHub API returned %s", resp.Status)
		}
		return fmt.Errorf("GitHub API returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	if readErr != nil {
		return readErr
	}
	trimmedBody := bytes.TrimSpace(respBody)
	if response != nil && len(trimmedBody) > 0 && (trimmedBody[0] == '{' || trimmedBody[0] == '[') {
		if err := json.Unmarshal(respBody, response); err != nil {
			return nil
		}
	}
	return nil
}

func (r *CellenzaReconciler) recordGitHubSuccess(ctx context.Context, c *platformv1alpha1.Cellenza, state, environmentURL string, commentID int64) {
	status := c.Status.GitHub
	if status == nil {
		status = &platformv1alpha1.GitHubIntegrationStatus{}
		c.Status.GitHub = status
	}
	status.DeploymentState = state
	status.LastNotifiedPhase = c.Status.Phase
	status.LastEnvironmentURL = environmentURL
	if commentID != 0 {
		status.CommentID = commentID
	}
	status.LastError = ""
	now := metav1.NewTime(time.Now())
	status.LastNotifiedAt = &now
	_ = r.Status().Update(ctx, c)
}

func (r *CellenzaReconciler) recordGitHubError(ctx context.Context, c *platformv1alpha1.Cellenza, err error) {
	status := c.Status.GitHub
	if status == nil {
		status = &platformv1alpha1.GitHubIntegrationStatus{}
		c.Status.GitHub = status
	}
	status.LastError = err.Error()
	_ = r.Status().Update(ctx, c)
}

func githubEnvironment(c *platformv1alpha1.Cellenza) string {
	if c.Spec.GitHub != nil && c.Spec.GitHub.Environment != "" {
		return c.Spec.GitHub.Environment
	}
	return "pr-" + strconv.Itoa(c.Spec.PRNumber)
}

func githubDeploymentStateForPhase(phase platformv1alpha1.EnvironmentPhase) string {
	switch phase {
	case platformv1alpha1.PhasePending:
		return "queued"
	case platformv1alpha1.PhaseProvisioning:
		return "in_progress"
	case platformv1alpha1.PhaseRunning:
		return "success"
	case platformv1alpha1.PhaseFailed:
		return "failure"
	case platformv1alpha1.PhaseTerminating:
		return "inactive"
	default:
		return "queued"
	}
}

func githubDescriptionForPhase(c *platformv1alpha1.Cellenza) string {
	switch c.Status.Phase {
	case platformv1alpha1.PhasePending:
		return "Preview environment is waiting for approval"
	case platformv1alpha1.PhaseProvisioning:
		return "Preview environment is being provisioned"
	case platformv1alpha1.PhaseRunning:
		return "Preview environment is ready"
	case platformv1alpha1.PhaseFailed:
		return "Preview environment failed"
	case platformv1alpha1.PhaseTerminating:
		return "Preview environment is being deleted"
	default:
		return "Preview environment is pending"
	}
}

func syncGitHubAfterStatus(ctx context.Context, r *CellenzaReconciler, c *platformv1alpha1.Cellenza, environmentURL string) {
	if !githubEnabled(c) {
		return
	}
	state := githubDeploymentStateForPhase(c.Status.Phase)
	commentOnReady := c.Status.Phase == platformv1alpha1.PhaseRunning && c.Spec.GitHub.CommentOnReady
	r.syncGitHub(ctx, c, state, environmentURL, githubDescriptionForPhase(c), commentOnReady)
}
