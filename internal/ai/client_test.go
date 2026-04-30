package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGenerateParsesResponse(t *testing.T) {
	client := NewClient("https://ai.example.test", "test-key", "")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"choices":[{"message":{"content":"{\"seed_sql\":\"INSERT INTO messages VALUES (1);\",\"test_script\":\"print('ok')\"}"}}]}`,
			)),
		}, nil
	})}

	resp, err := client.Generate(context.Background(), GenerateRequest{
		PRDiff:   "diff --git a/app.py b/app.py",
		DBSchema: "create table messages(id int);",
		AppURL:   "http://app:8080",
		Branch:   "test",
		PRNumber: 21,
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.SeedSQL != "INSERT INTO messages VALUES (1);" {
		t.Fatalf("unexpected seed sql: %q", resp.SeedSQL)
	}
	if resp.TestScript != "print('ok')" {
		t.Fatalf("unexpected test script: %q", resp.TestScript)
	}
}

func TestTruncateAppendsSuffix(t *testing.T) {
	got := truncate(strings.Repeat("a", 10), 5)
	if !strings.Contains(got, "... (truncated)") {
		t.Fatalf("expected truncated suffix, got %q", got)
	}
}

func TestTruncate_withinLimit(t *testing.T) {
	input := "short"
	got := truncate(input, 100)
	if got != input {
		t.Fatalf("expected unchanged string %q, got %q", input, got)
	}
}

func TestGenerate_http500(t *testing.T) {
	client := NewClient("https://ai.example.test", "test-key", "")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("internal error")),
		}, nil
	})}

	_, err := client.Generate(context.Background(), GenerateRequest{PRNumber: 1})
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error message should mention 500, got: %v", err)
	}
}

func TestGenerate_emptyChoices(t *testing.T) {
	client := NewClient("https://ai.example.test", "test-key", "")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"choices":[]}`)),
		}, nil
	})}

	_, err := client.Generate(context.Background(), GenerateRequest{PRNumber: 1})
	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
}

func TestGenerate_invalidInnerJSON(t *testing.T) {
	client := NewClient("https://ai.example.test", "test-key", "")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"not-json"}}]}`)),
		}, nil
	})}

	_, err := client.Generate(context.Background(), GenerateRequest{PRNumber: 1})
	if err == nil {
		t.Fatal("expected error for invalid inner JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}
}

func TestNewClient_defaultModel(t *testing.T) {
	c := NewClient("https://api.openai.com/v1", "sk-test", "")
	if c.Model != defaultModel {
		t.Errorf("expected default model %q, got %q", defaultModel, c.Model)
	}
}

func TestNewClient_trailingSlashStripped(t *testing.T) {
	c := NewClient("https://api.openai.com/v1/", "sk-test", "")
	if strings.HasSuffix(c.BaseURL, "/") {
		t.Errorf("BaseURL should not end with slash, got %q", c.BaseURL)
	}
}
