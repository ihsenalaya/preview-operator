package evidence

import "testing"

func TestRedact(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantChanged bool
		mustContain string
		mustNotHave string
	}{
		{
			name:        "plain text untouched",
			in:          "migration job failed: relation already exists",
			wantChanged: false,
			mustContain: "relation already exists",
		},
		{
			name:        "password assignment",
			in:          "DB_PASSWORD=s3cr3tValue connecting...",
			wantChanged: true,
			mustContain: RedactionPlaceholder,
			mustNotHave: "s3cr3tValue",
		},
		{
			name:        "token colon form",
			in:          `{"api_token": "ghp_abcdef123456"}`,
			wantChanged: true,
			mustContain: RedactionPlaceholder,
			mustNotHave: "ghp_abcdef123456",
		},
		{
			name:        "api key with dash",
			in:          "api-key = AKIAIOSFODNN7EXAMPLE",
			wantChanged: true,
			mustContain: RedactionPlaceholder,
			mustNotHave: "AKIAIOSFODNN7EXAMPLE",
		},
		{
			name:        "bearer token",
			in:          "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantChanged: true,
			mustContain: RedactionPlaceholder,
			mustNotHave: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		},
		{
			name:        "credentials in connection url",
			in:          "DATABASE_URL=postgres://appuser:supersecret@postgres:5432/appdb",
			wantChanged: true,
			mustContain: RedactionPlaceholder,
			mustNotHave: "supersecret",
		},
		{
			name:        "url without credentials is kept",
			in:          "calling http://backend:8080/api/products",
			wantChanged: false,
			mustContain: "backend:8080",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := Redact(tc.in)
			if changed != tc.wantChanged {
				t.Fatalf("Redact(%q) changed = %v, want %v (got %q)", tc.in, changed, tc.wantChanged, got)
			}
			if tc.mustContain != "" && !contains(got, tc.mustContain) {
				t.Errorf("Redact(%q) = %q, want it to contain %q", tc.in, got, tc.mustContain)
			}
			if tc.mustNotHave != "" && contains(got, tc.mustNotHave) {
				t.Errorf("Redact(%q) = %q, must NOT contain the secret %q", tc.in, got, tc.mustNotHave)
			}
		})
	}
}

func TestRedactConnectionURLKeepsUserAndHost(t *testing.T) {
	got, changed := Redact("postgres://appuser:supersecret@postgres:5432/appdb")
	if !changed {
		t.Fatal("expected redaction of connection-url credentials")
	}
	if !contains(got, "appuser") || !contains(got, "postgres:5432") {
		t.Errorf("redaction must keep user and host for readability, got %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
