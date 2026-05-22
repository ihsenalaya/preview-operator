package diagnosis

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

// OpenAIClient is an OpenAI-compatible chat-completions LLMClient. It follows the
// same provider-neutral HTTP shape the operator already uses in internal/ai, so
// the diagnostic harness can point at any OpenAI-compatible endpoint (OpenAI,
// Azure OpenAI, or a local gateway).
//
// Temperature is fixed at 0 and is not configurable: the controlled evaluation
// requires the diagnostic step to be as reproducible as the model allows
// (RQ2/RQ4 — see research-questions.md).
type OpenAIClient struct {
	// BaseURL is the API root, e.g. https://api.openai.com/v1.
	BaseURL string
	// APIKey authenticates the request.
	APIKey string
	// ModelID is the model identifier sent in the request and recorded on the
	// Result for reproducibility.
	ModelID string
	// HTTPClient is optional; a 90s-timeout client is used when nil.
	HTTPClient *http.Client
}

// NewOpenAIClient builds an OpenAIClient with a default HTTP client.
func NewOpenAIClient(baseURL, apiKey, model string) *OpenAIClient {
	return &OpenAIClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		ModelID:    model,
		HTTPClient: &http.Client{Timeout: 90 * time.Second},
	}
}

// Model returns the configured model identifier.
func (c *OpenAIClient) Model() string { return c.ModelID }

// Complete sends the system and user prompts to the chat-completions endpoint
// at temperature 0 and returns the model's text content.
func (c *OpenAIClient) Complete(ctx context.Context, system, user string) (string, error) {
	if c.BaseURL == "" {
		return "", fmt.Errorf("openai client: BaseURL is empty")
	}
	body, err := json.Marshal(map[string]any{
		"model": c.ModelID,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", err
	}

	endpoint := c.BaseURL + "/chat/completions"
	isAzure := strings.Contains(c.BaseURL, "azure.com")
	if isAzure {
		endpoint += "?api-version=2024-10-21"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if isAzure {
		req.Header.Set("api-key", c.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("invalid LLM response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("LLM response contained no choices")
	}
	return apiResp.Choices[0].Message.Content, nil
}
