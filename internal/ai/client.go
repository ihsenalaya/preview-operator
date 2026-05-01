package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultModel = "gpt-4o-mini"

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
	systemPrompt := `You are a developer tool for Kubernetes preview environments.
Given a pull request diff and optionally a database schema, generate:
1. seed_sql: SQL INSERT statements that populate the preview database with realistic data
   relevant to the PR changes. Use only tables that exist in the schema.
   If no schema is provided, return an empty string.
2. test_script: A Python script using the 'requests' library that tests the HTTP endpoints
   modified or added by the PR. The script must use the APP_URL environment variable as base URL.
   IMPORTANT: Only test JSON/API endpoints (endpoints that call jsonify() or return JSON).
   Skip form-based endpoints that render HTML templates or return redirects — do not test those.
   Inspect the diff carefully: if a route uses render_template, redirect, or returns plain HTML,
   exclude it from the test script entirely.
   Print each test result on a separate line as: "PASS <method> <path>" or "FAIL <method> <path> - <reason>".
   Exit with code 1 if any test fails.

Respond ONLY with valid JSON: {"seed_sql": "...", "test_script": "..."}`

	if req.ExtraInstructions != "" {
		systemPrompt += "\n\nAdditional instructions:\n" + req.ExtraInstructions
	}

	userPrompt := fmt.Sprintf(
		"Branch: %s\nPR #%d\n\nDiff:\n%s\n\nDB Schema:\n%s\n\nApp URL env var: APP_URL=%s",
		req.Branch,
		req.PRNumber,
		truncate(req.PRDiff, 8000),
		truncate(req.DBSchema, 4000),
		req.AppURL,
	)

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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... (truncated)"
}
