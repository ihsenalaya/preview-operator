package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestBuildSystemPromptGuidesTestsToAPIEndpoints(t *testing.T) {
	prompt := buildSystemPrompt("", "Only test stable product endpoints.")

	for _, want := range []string{
		"Only test JSON/API endpoints",
		"Do not assume HTTP 200 for successful POST, PUT, or PATCH requests.",
		"201 Created",
		"/api/",
		"request.form",
		"redirect()",
		"/add-product",
		"requests.post(..., json=...)",
		"Never hardcode row identifiers",
		"omit that",
		"Do not invent endpoints",
		"/api/orders does not imply",
		"Only test stable product endpoints.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildSystemPromptUsesConfigMapTemplateWhenProvided(t *testing.T) {
	prompt := buildSystemPrompt("Custom system prompt from Helm.", "Prefer 201 for create endpoints.")

	for _, want := range []string{
		"Custom system prompt from Helm.",
		"Additional instructions:",
		"Prefer 201 for create endpoints.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "You are a developer tool for Kubernetes preview environments.") {
		t.Fatalf("expected custom system prompt to replace the default template:\n%s", prompt)
	}
}

func TestSummarizeRoutesFromDiffClassifiesBrowserAndAPIEndpoints(t *testing.T) {
	diff := `diff --git a/app.py b/app.py
@@
+@app.route("/add-product", methods=["POST"])
+def add_product():
+    name = request.form.get("name")
+    return redirect("/")
+
+@app.route("/api/products", methods=["POST"])
+def api_create_product():
+    data = request.get_json(silent=True) or {}
+    return jsonify({"id": 1}), 201
+
+@app.route("/healthz")
+def healthz():
+    return "ok", 200
-@app.route("/old-form", methods=["POST"])
-def old_form():
-    return redirect("/")
`

	summary := summarizeRoutesFromDiff(diff)
	for _, want := range []string{
		"/add-product: browser/form HTML or redirect endpoint",
		"/api/products: JSON/API candidate",
		"/healthz: health endpoint",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("route summary missing %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "/old-form") {
		t.Fatalf("route summary should ignore removed routes:\n%s", summary)
	}
}

func TestBuildUserPromptIncludesRouteHints(t *testing.T) {
	prompt := buildUserPrompt(GenerateRequest{
		PRDiff: `+@app.route("/add-product", methods=["POST"])
+def add_product():
+    name = request.form.get("name")
+    return redirect("/")`,
		AppURL:   "http://app:80",
		Branch:   "feature/test",
		PRNumber: 25,
	})

	if !strings.Contains(prompt, "Route hints from the PR diff") {
		t.Fatalf("expected user prompt to include route hints:\n%s", prompt)
	}
	if !strings.Contains(prompt, "/add-product: browser/form HTML or redirect endpoint") {
		t.Fatalf("expected user prompt to classify /add-product as browser/form:\n%s", prompt)
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
	var calls int
	client := NewClient("https://ai.example.test", "test-key", "")
	client.retryBaseDelay = time.Millisecond // 500 is retried; keep the test fast
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
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
	if calls != maxAttempts {
		t.Errorf("transient 500 should be retried %d times, got %d", maxAttempts, calls)
	}
}

// TestGenerateRetriesOn429 checks that AI enrichment survives a throttled
// endpoint — the failure mode that left previews with an empty catalogue.
func TestGenerateRetriesOn429(t *testing.T) {
	var calls int
	client := NewClient("https://ai.example.test", "test-key", "")
	client.retryBaseDelay = time.Millisecond
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls <= 2 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Too Many Requests"}}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`{"choices":[{"message":{"content":"{\"seed_sql\":\"\",\"test_script\":\"print('ok')\"}"}}]}`,
			)),
		}, nil
	})}

	resp, err := client.Generate(context.Background(), GenerateRequest{PRNumber: 1})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if resp.TestScript != "print('ok')" {
		t.Errorf("unexpected test script: %q", resp.TestScript)
	}
	if calls != 3 {
		t.Errorf("transport called %d times, want 3 (two 429s then success)", calls)
	}
}

// TestGenerateGivesUpAfter429s checks that sustained throttling fails after
// exactly maxAttempts rather than looping forever.
func TestGenerateGivesUpAfter429s(t *testing.T) {
	var calls int
	client := NewClient("https://ai.example.test", "test-key", "")
	client.retryBaseDelay = time.Millisecond
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Too Many Requests"}}`)),
		}, nil
	})}

	if _, err := client.Generate(context.Background(), GenerateRequest{PRNumber: 1}); err == nil {
		t.Fatal("Generate should fail when every attempt is throttled")
	}
	if calls != maxAttempts {
		t.Errorf("transport called %d times, want %d", calls, maxAttempts)
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
