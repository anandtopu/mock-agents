package recording

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/mockagents/mockagents/internal/storage"
)

// builtinSecretPatterns are high-confidence, self-delimiting credential shapes
// masked by default (in addition to storage.SanitizeBody's sk-/key-/Bearer
// prefixes). Each matches a whole token, so replacing it with "***" can never
// break surrounding JSON structure. They are applied to JSON string *values*
// only (see redactBody), so a match can never rename a key or corrupt framing.
var builtinSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                                           // AWS access key id
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),                                        // GitHub PAT (classic)
	regexp.MustCompile(`gh[osu]_[A-Za-z0-9]{36}`),                                    // GitHub oauth/server/user tokens
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`),                               // GitHub fine-grained PAT
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),                               // Slack tokens
	regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`),                                     // Google/Gemini API key
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), // JWT
}

// Redactor masks secrets in a recorded Interaction before it is appended to a
// cassette (R-03). It applies storage.SanitizeBody's prefix masking plus a set
// of built-in credential-shape patterns and any caller-supplied extra patterns.
//
// Redaction is structure-preserving: for JSON bodies it walks the document and
// rewrites string *values* only, so a pattern can never break JSON framing,
// rename a key, or otherwise corrupt the body. The cassette therefore stays
// loadable AND replay-faithful while secrets are removed. The Redactor never
// touches the response forwarded to the client; only the stored copy is masked.
//
// Coverage is best-effort: it catches the common provider key formats and any
// caller-supplied pattern, but is not a guarantee that every secret is gone —
// review a cassette before committing it.
type Redactor struct {
	patterns []*regexp.Regexp
}

// NewRedactor builds a Redactor. extraPatterns are additional regexps (from
// --redact-pattern) compiled and applied on top of the built-in set. Returns an
// error if any pattern fails to compile.
func NewRedactor(extraPatterns []string) (*Redactor, error) {
	r := &Redactor{}
	for _, p := range extraPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("compiling redact pattern %q: %w", p, err)
		}
		r.patterns = append(r.patterns, re)
	}
	return r, nil
}

// Apply masks secrets in every recorded surface of it: request/response bodies,
// stream-event payloads, and captured header values. Safe to call on a nil
// Redactor or a nil Interaction (both no-ops).
func (r *Redactor) Apply(it *Interaction) {
	if r == nil || it == nil {
		return
	}
	it.RequestBody = r.redactBody(it.RequestBody)
	it.ResponseBody = r.redactBody(it.ResponseBody)
	for i := range it.StreamEvents {
		it.StreamEvents[i].Data = r.redactStreamData(it.StreamEvents[i].Data)
	}
	redactHeaderValues(r, it.RequestHeaders)
	redactHeaderValues(r, it.ResponseHeaders)
}

// redactString masks secrets in a single plain string: prefix masking, the
// built-in credential patterns, then any caller-supplied patterns.
func (r *Redactor) redactString(s string) string {
	out := storage.SanitizeBody(s)
	for _, re := range builtinSecretPatterns {
		out = re.ReplaceAllString(out, "***")
	}
	for _, re := range r.patterns {
		out = re.ReplaceAllString(out, "***")
	}
	return out
}

// redactBody masks secrets in a JSON body without altering its structure. It
// walks the decoded document and rewrites string values only, so a pattern can
// never break JSON validity or rename a key. Bodies with no match are returned
// byte-for-byte (no churn); non-JSON bodies are masked as a raw string (there
// is no structure to protect).
func (r *Redactor) redactBody(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return b
	}
	// Fast path: nothing anywhere in the body matches → return verbatim so a
	// cassette of secret-free interactions is byte-identical with or without
	// --redact (no key reordering, no re-encoding).
	if r.redactString(string(b)) == string(b) {
		return b
	}
	// UseNumber keeps large integers (token ids, timestamps) exact through the
	// decode/encode round-trip instead of degrading them to float64.
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err == nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false) // preserve <, >, & in content verbatim
		if err := enc.Encode(r.redactJSONValue(v)); err == nil {
			return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n"))
		}
	}
	// Not JSON (or un-encodable): no structure to break, mask the raw string.
	return json.RawMessage(r.redactString(string(b)))
}

// redactJSONValue recurses through a decoded JSON value, masking string leaves
// only. Maps and slices are walked in place; numbers, bools, and null pass
// through untouched.
func (r *Redactor) redactJSONValue(v any) any {
	switch t := v.(type) {
	case string:
		return r.redactString(t)
	case []any:
		for i := range t {
			t[i] = r.redactJSONValue(t[i])
		}
		return t
	case map[string]any:
		for k, val := range t {
			t[k] = r.redactJSONValue(val)
		}
		return t
	default:
		return v
	}
}

// redactStreamData masks secrets in an SSE chunk. SSE is line-oriented
// (`data: {json}`); each line's JSON payload is redacted through the same
// structure-preserving value walk as a body, so a custom pattern can never
// corrupt the frame. Non-JSON payloads get prefix masking only (structure-safe).
// Limitation: a secret split across two network chunks is not caught, since each
// chunk is redacted independently.
func (r *Redactor) redactStreamData(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		payload := strings.TrimPrefix(line, "data: ")
		prefix := line[:len(line)-len(payload)]
		if payload != "" && json.Valid([]byte(payload)) {
			lines[i] = prefix + string(r.redactBody(json.RawMessage(payload)))
		} else {
			// No JSON structure to protect; prefix masking is structure-safe.
			lines[i] = storage.SanitizeBody(line)
		}
	}
	return strings.Join(lines, "\n")
}

func redactHeaderValues(r *Redactor, h map[string]string) {
	for k, v := range h {
		h[k] = r.redactString(v)
	}
}
