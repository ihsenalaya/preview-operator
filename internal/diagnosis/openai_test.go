package diagnosis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestOpenAIClientRetriesOn429 checks that a throttled request is retried and
// eventually succeeds — the failure mode that dropped every LLM row of the
// failure-provenance matrix.
func TestOpenAIClientRetriesOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Too Many Requests"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "key", "gpt-4o-mini")
	c.retryBaseDelay = time.Millisecond

	got, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got != "ok" {
		t.Errorf("content = %q, want %q", got, "ok")
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("server received %d calls, want 3 (two 429s then success)", n)
	}
}

// TestOpenAIClientGivesUpAfterMaxAttempts checks that sustained throttling fails
// after exactly maxAttempts rather than looping forever.
func TestOpenAIClientGivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Too Many Requests"}}`))
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "key", "gpt-4o-mini")
	c.retryBaseDelay = time.Millisecond

	if _, err := c.Complete(context.Background(), "sys", "user"); err == nil {
		t.Fatal("Complete should fail when every attempt is throttled")
	}
	if n := calls.Load(); int(n) != maxAttempts {
		t.Errorf("server received %d calls, want %d", n, maxAttempts)
	}
}

// TestOpenAIClientDoesNotRetryClientError checks that a non-transient 4xx fails
// fast: retrying a malformed request would only waste the backoff budget.
func TestOpenAIClientDoesNotRetryClientError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "key", "gpt-4o-mini")
	c.retryBaseDelay = time.Millisecond

	if _, err := c.Complete(context.Background(), "sys", "user"); err == nil {
		t.Fatal("Complete should fail on a 400")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("server received %d calls, want 1 (400 is not retried)", n)
	}
}

// TestOpenAIClientHonoursRetryAfter checks that a Retry-After header overrides
// the exponential backoff: the base delay is set absurdly high, so the call can
// only finish quickly if the 1s header value is used instead.
func TestOpenAIClientHonoursRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "key", "gpt-4o-mini")
	c.retryBaseDelay = time.Hour // the test would hang if Retry-After were ignored

	done := make(chan struct{})
	var got string
	var err error
	go func() {
		got, err = c.Complete(context.Background(), "sys", "user")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Complete did not honour Retry-After (still backing off after 30s)")
	}
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got != "done" {
		t.Errorf("content = %q, want %q", got, "done")
	}
}
