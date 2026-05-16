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

const defaultTemperature = 0.2

// Client is an OpenAI-compatible AI client.
type Client struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
	HTTPClient  *http.Client
}

// NewClient creates a new AI client.
func NewClient(baseURL, apiKey, model string) *Client {
	if model == "" {
		model = defaultModel
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIKey:      apiKey,
		Model:       model,
		Temperature: defaultTemperature,
		HTTPClient:  &http.Client{Timeout: 60 * time.Second},
	}
}

// GenerateRequest holds the context sent to the AI.
type GenerateRequest struct {
	PRDiff            string // git diff of the pull request (truncated to 8000 chars)
	DBSchema          string // pg_dump --schema-only output (empty if no database)
	AppURL            string // preview app URL (e.g. http://app:8080)
	Branch            string
	PRNumber          int
	SystemPrompt      string // full system prompt template loaded from ConfigMap (optional)
	ExtraInstructions string // custom instructions from the ai-prompt ConfigMap (optional)
}

// GenerateResponse holds the AI-generated content.
type GenerateResponse struct {
	SeedSQL    string // SQL INSERT statements to populate the preview database
	TestScript string // Python script using requests to test the app
}

// Generate calls the AI API and returns seed SQL and a test script.
// If the generated test script contains SQL syntax (a common LLM mistake), it
// retries once with an explicit correction prompt before giving up.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	result, err := c.generate(ctx, req)
	if err != nil {
		return nil, err
	}
	if violation := sqlInPythonViolation(result.TestScript); violation != "" {
		req.ExtraInstructions = "CORRECTION REQUIRED: The previous test_script contained SQL syntax inside Python code (" + violation + "). " +
			"This causes a Python SyntaxError. Fix: replace every SQL expression with an HTTP GET or POST call to discover or create the resource via the API. " +
			req.ExtraInstructions
		result, err = c.generate(ctx, req)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (c *Client) generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	systemPrompt := buildSystemPrompt(req.SystemPrompt, req.ExtraInstructions)
	userPrompt := buildUserPrompt(req)

	body, err := json.Marshal(map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature":     c.Temperature,
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return nil, err
	}

	endpoint := c.BaseURL + "/chat/completions"
	isAzure := strings.Contains(c.BaseURL, "azure.com")
	if isAzure {
		endpoint += "?api-version=2024-10-21"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if isAzure {
		httpReq.Header.Set("api-key", c.APIKey)
	} else {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

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

// sqlInPythonViolation returns a short description of the first SQL-in-Python
// pattern found, or an empty string if the script looks clean.
var sqlKeywordRE = regexp.MustCompile(`(?i)\(\s*(SELECT|INSERT\s+INTO|UPDATE\s+\w|DELETE\s+FROM)\s+`)

func sqlInPythonViolation(script string) string {
	if m := sqlKeywordRE.FindString(script); m != "" {
		return strings.TrimSpace(m)
	}
	return ""
}

const defaultSystemPrompt = `You are a developer tool for Kubernetes preview environments.
Given a pull request diff and optionally a database schema, generate:
1. seed_sql: SQL INSERT statements that populate the preview database with realistic data
   relevant to the PR changes. Use only tables that exist in the schema.
   If no schema is provided, return an empty string.
2. test_script: A Python script using the 'requests' library that tests the HTTP endpoints
   modified or added by the PR. The script must use the APP_URL environment variable as base URL.
   IMPORTANT: Only test JSON/API endpoints.
   Do not assume HTTP 200 for successful POST, PUT, or PATCH requests.
   For resource-creation endpoints, prefer the explicit status code shown in the diff and expect
   201 Created when the handler indicates creation semantics.
   Prefer routes whose path starts with /api/ and whose handler uses jsonify(), request.get_json(),
   or another explicit JSON response.
   The test_script runs in a plain Python environment with no database access.
   NEVER write any SQL syntax inside test_script (no SELECT, INSERT, UPDATE, DELETE).
   NEVER use SQL subqueries, raw SQL strings, or psycopg2/SQLAlchemy in test_script.
   Never hardcode row identifiers such as category_id=1 or product_id=1 unless the script created
   that row itself earlier in the same execution.
   If a request depends on another resource (e.g. a category_id), always use an HTTP GET endpoint
   to discover an existing ID, or create the prerequisite with a POST endpoint first, then reuse
   the returned id. If no API endpoint exists to create or discover the prerequisite, omit that
   field entirely instead of guessing or constructing a SQL query.
   Do not invent endpoints. A collection route such as /api/orders does not imply that
   /api/orders/<id> exists. Only call item-by-id routes when they are explicitly present in the diff,
   route hints, or another verified API response.
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

func buildSystemPrompt(basePrompt, extraInstructions string) string {
	systemPrompt := strings.TrimSpace(basePrompt)
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}

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

	for raw := range strings.SplitSeq(diff, "\n") {
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
