package evidence

import "regexp"

// RedactionPlaceholder replaces any secret-bearing value found in evidence content.
const RedactionPlaceholder = "[REDACTED]"

// redactionRules pairs a pattern with its submatch-aware replacement. Each rule
// keeps the surrounding context (the key, the URL scheme/user) and replaces only
// the secret value, so the redacted evidence stays readable.
//
// Order matters: the Bearer rule runs before the generic key:value rule, so an
// "Authorization: Bearer <token>" header has its token redacted before the
// key:value rule rewrites the word that follows "Authorization:".
var redactionRules = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{
		// Bearer tokens in Authorization headers.
		regexp.MustCompile(`(?i)\b(bearer\s+)([A-Za-z0-9._\-]{8,})`),
		`${1}` + RedactionPlaceholder,
	},
	{
		// Credentials embedded in a connection URL: scheme://user:password@host.
		regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^:/@\s]+:)([^@/\s]+)(@)`),
		`${1}` + RedactionPlaceholder + `${3}`,
	},
	{
		// key = value / key: value for secret-bearing keys. No left word boundary:
		// the keyword must still be catchable after an underscore (e.g. DB_PASSWORD,
		// api_token), and the required [:=] separator keeps the match precise.
		regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|apikey|authorization|auth[_-]?token|access[_-]?key|client[_-]?secret)(\s*["']?\s*[:=]\s*["']?)([^\s"',;]+)`),
		`${1}${2}` + RedactionPlaceholder,
	},
}

// Redact replaces secret-bearing values — passwords, tokens, API keys, bearer
// tokens, and credentials embedded in connection URLs — with RedactionPlaceholder.
// It returns the redacted text and whether any redaction was applied.
//
// Redaction is applied before an evidence item's ID is computed, so a redacted and
// an unredacted run of the same evidence still yield the same deterministic ID.
func Redact(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	out := text
	for _, rule := range redactionRules {
		out = rule.pattern.ReplaceAllString(out, rule.replacement)
	}
	return out, out != text
}
