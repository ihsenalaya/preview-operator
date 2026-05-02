package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const defaultModel = "gpt-4o-mini"

var routeDecoratorRE = regexp.MustCompile(`@\w+\.(?:route|get|post|put|patch|delete)\(\s*["']([^"']+)["']`)

// Client is an OpenAI-compatible AI client.
type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// NewClient creates a new AI client.
func NewClient(baseURL, apiKey, model string) *Client {
	if model == "" {
		model = defaultModel
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// GenerateRequest holds the context sent to the AI.
type GenerateRequest struct {
	PRDiff            string // git diff of the pull request (truncated to 8000 chars)
	DBSchema          string // pg_dump --schema-only output (empty if no database)
	AppURL            string // preview app URL (e.g. http://app:8080)
	Branch            string
	PRNumber          int
	ExtraInstructions string // custom instructions from the ai-prompt ConfigMap (optional)
}

// GenerateResponse holds the AI-generated content.
type GenerateResponse struct {
	SeedSQL    string // SQL INSERT statements to populate the preview database
	TestScript string // Python script using requests to test the app
}

// Generate calls the AI API and returns seed SQL and a test script.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	systemPrompt := buildSystemPrompt(req.ExtraInstructions)
	userPrompt := buildUserPrompt(req)

	body, err := json.Marshal(map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature":     0.2,
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AI API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil || len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("invalid AI response: %w", err)
	}

	var generated struct {
		SeedSQL    string `json:"seed_sql"`
		TestScript string `json:"test_script"`
	}
	if err := json.Unmarshal([]byte(apiResp.Choices[0].Message.Content), &generated); err != nil {
		return nil, fmt.Errorf("failed to parse AI JSON response: %w", err)
	}

	return &GenerateResponse{
		SeedSQL:    generated.SeedSQL,
		TestScript: generated.TestScript,
	}, nil
}

func buildSystemPrompt(extraInstructions string) string {
	systemPrompt := `You are a developer tool for Kubernetes preview environments.
Given a pull request diff and optionally a database schema, generate:
1. seed_sql: SQL INSERT statements that populate the preview database with realistic data
   relevant to the PR changes. Use only tables that exist in the schema.
   If no schema is provided, return an empty string.
2. test_script: A Python script using the 'requests' library that tests the HTTP endpoints
   modified or added by the PR. The script must use the APP_URL environment variable as base URL.
   IMPORTANT: Only test JSON/API endpoints.
   Prefer routes whose path starts with /api/ and whose handler uses jsonify(), request.get_json(),
   or another explicit JSON response.
   Health endpoints such as /healthz or /ping may be tested with status or plain-text assertions
   when they are explicitly present in the diff.
   Skip browser/form endpoints, even if they use POST. Any route that reads request.form,
   renders HTML, calls render_template(), calls render_page(), or returns redirect() is not an API
   test target.
   Do not invent JSON contracts for form routes. For example, a route like /add-product that reads
   request.form and redirects must not be tested with requests.post(..., json=...); use an existing
   /api/... endpoint instead if one is available.
   Do not assert on root pages or localized/static HTML text.
   Follow the route hints in the user message over route-name guesses.
   Print each test result on a separate line as: "PASS <method> <path>" or "FAIL <method> <path> - <reason>".
   Exit with code 1 if any test fails.

Respond ONLY with valid JSON: {"seed_sql": "...", "test_script": "..."}`

	if strings.TrimSpace(extraInstructions) != "" {
		systemPrompt += "\n\nAdditional instructions:\n" + strings.TrimSpace(extraInstructions)
	}

	return systemPrompt
}

func buildUserPrompt(req GenerateRequest) string {
	userPrompt := fmt.Sprintf(
		"Branch: %s\nPR #%d\n\nDiff:\n%s\n\nDB Schema:\n%s\n\nApp URL env var: APP_URL=%s",
		req.Branch,
		req.PRNumber,
		truncate(req.PRDiff, 8000),
		truncate(req.DBSchema, 4000),
		req.AppURL,
	)

	if routeHints := summarizeRoutesFromDiff(req.PRDiff); routeHints != "" {
		userPrompt += "\n\n" + routeHints
	}

	return userPrompt
}

type routeHint struct {
	path    string
	json    bool
	browser bool
}

func summarizeRoutesFromDiff(diff string) string {
	routes := map[string]*routeHint{}
	var order []string
	var current *routeHint

	for _, raw := range strings.Split(diff, "\n") {
		line, ok := diffContentLine(raw)
		if !ok {
			continue
		}

		if match := routeDecoratorRE.FindStringSubmatch(line); len(match) == 2 {
			path := match[1]
			current = routes[path]
			if current == nil {
				current = &routeHint{path: path}
				routes[path] = current
				order = append(order, path)
			}
			continue
		}
		if current == nil {
			continue
		}

		lower := strings.ToLower(line)
		if strings.Contains(lower, "jsonify(") ||
			strings.Contains(lower, "request.get_json") ||
			strings.Contains(lower, "application/json") {
			current.json = true
		}
		if strings.Contains(lower, "request.form") ||
			strings.Contains(lower, "redirect(") ||
			strings.Contains(lower, "render_template(") ||
			strings.Contains(lower, "render_page(") ||
			strings.Contains(lower, "text/html") ||
			strings.Contains(lower, "<!doctype html") ||
			strings.Contains(lower, "<html") {
			current.browser = true
		}
	}

	if len(order) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Route hints from the PR diff:\n")
	for _, path := range order {
		hint := routes[path]
		switch {
		case hint.browser:
			fmt.Fprintf(&b, "- %s: browser/form HTML or redirect endpoint; do not create JSON/API tests for this route.\n", path)
		case hint.json || strings.HasPrefix(path, "/api/"):
			fmt.Fprintf(&b, "- %s: JSON/API candidate; test it with requests and JSON assertions only if it is relevant to the PR.\n", path)
		case path == "/healthz" || path == "/ping":
			fmt.Fprintf(&b, "- %s: health endpoint; status or exact plain-text assertions are acceptable.\n", path)
		default:
			fmt.Fprintf(&b, "- %s: no JSON evidence detected; test it only if the handler clearly returns JSON.\n", path)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func diffContentLine(line string) (string, bool) {
	if strings.HasPrefix(line, "diff --git ") ||
		strings.HasPrefix(line, "index ") ||
		strings.HasPrefix(line, "@@") ||
		strings.HasPrefix(line, "+++") ||
		strings.HasPrefix(line, "---") {
		return "", false
	}
	if line == "" {
		return "", true
	}
	switch line[0] {
	case '+', ' ':
		return line[1:], true
	case '-':
		return "", false
	default:
		return line, true
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... (truncated)"
}
