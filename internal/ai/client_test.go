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
