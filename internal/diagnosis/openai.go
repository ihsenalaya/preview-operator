package diagnosis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxAttempts caps how many times Complete sends a request that is throttled or
// transiently failing. Azure OpenAI answers 429 when diagnoses are issued in
// close succession, as the evaluation matrix does; retrying with exponential
// backoff keeps the matrix from dropping LLM rows. The default backoff between
// six attempts spans 1+2+4+8+16 = 31s.
const maxAttempts = 6

// defaultRetryBaseDelay is the first backoff interval; each further attempt
// doubles it. It is a field on the client only so tests can shorten it.
const defaultRetryBaseDelay = 1 * time.Second

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
	// retryBaseDelay overrides the first backoff interval; zero means
	// defaultRetryBaseDelay. It exists so tests need not wait whole seconds.
	retryBaseDelay time.Duration
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

// retryableError marks an LLM API failure worth retrying — a 429, a transient
// 5xx, or a network error. Non-retryable errors (4xx other than 429, malformed
// responses) are returned directly so the caller fails fast.
type retryableError struct{ err error }

func (e retryableError) Error() string { return e.err.Error() }
func (e retryableError) Unwrap() error { return e.err }

// Complete sends the system and user prompts to the chat-completions endpoint
// at temperature 0 and returns the model's text content.
//
// Azure OpenAI throttles with HTTP 429 when diagnoses are issued in close
// succession, as the evaluation matrix does. Complete retries 429 and transient
// 5xx/network failures with exponential backoff, honouring a Retry-After header
// when the service supplies one, and gives up after maxAttempts.
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

	base := c.retryBaseDelay
	if base <= 0 {
		base = defaultRetryBaseDelay
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		content, retryAfter, err := c.doRequest(ctx, endpoint, isAzure, body)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if !errors.As(err, &retryableError{}) || attempt == maxAttempts {
			return "", err
		}
		delay := retryAfter
		if delay <= 0 {
			delay = base << (attempt - 1)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}
	}
	return "", lastErr
}

// doRequest performs one chat-completions call. It returns the model content on
// success, or a retryableError plus a Retry-After hint when the failure is
// transient.
func (c *OpenAIClient) doRequest(ctx context.Context, endpoint string, isAzure bool, body []byte) (string, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
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
		return "", 0, retryableError{err} // connection resets and timeouts are transient
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, retryableError{err}
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := fmt.Errorf("LLM API error %d: %s", resp.StatusCode, string(respBody))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return "", parseRetryAfter(resp.Header), retryableError{apiErr}
		}
		return "", 0, apiErr
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", 0, fmt.Errorf("invalid LLM response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return "", 0, fmt.Errorf("LLM response contained no choices")
	}
	return apiResp.Choices[0].Message.Content, 0, nil
}

// parseRetryAfter reads a Retry-After header in delta-seconds form and returns
// it as a duration, or 0 when the header is absent or unparsable.
func parseRetryAfter(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}
