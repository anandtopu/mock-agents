package recording

import "encoding/json"

// Matcher relaxes replay request matching by ignoring a set of top-level request
// body fields. A request that differs from a recorded interaction only in those
// fields (e.g. temperature, seed, stream, metadata) still matches.
//
// Ignoring is a REPLAY-time concept: the cassette on disk is unchanged and each
// interaction's stored Hash is the full-body hash. The Replay derives a separate
// "match key" by stripping the ignored fields from both the incoming request and
// each stored interaction before hashing (see Replay.buildMatchIndex). When no
// fields are ignored the matcher is inactive and Key is identical to HashRequest.
type Matcher struct {
	IgnoreFields []string
	ignoreSet    map[string]struct{}
}

// NewMatcher builds a Matcher that ignores the given top-level body fields.
func NewMatcher(ignoreFields []string) *Matcher {
	set := make(map[string]struct{}, len(ignoreFields))
	for _, f := range ignoreFields {
		set[f] = struct{}{}
	}
	return &Matcher{IgnoreFields: ignoreFields, ignoreSet: set}
}

// active reports whether the matcher actually strips anything. A nil or
// empty-field matcher is inactive, so the exact-hash path stays the default.
func (m *Matcher) active() bool {
	return m != nil && len(m.ignoreSet) > 0
}

// Key returns the match key for a request: the ignored top-level fields are
// removed from the body, then the result is hashed like any other request. When
// the matcher is inactive (or the body is not a JSON object) this is identical
// to HashRequest over the original body.
func (m *Matcher) Key(method, path string, body []byte) string {
	return HashRequest(method, path, m.strip(body))
}

// strip removes the ignored top-level fields from a JSON-object body and
// re-encodes it canonically. A non-object body (array, scalar, null, or non-JSON)
// is returned unchanged — there are no top-level fields to remove.
func (m *Matcher) strip(body []byte) []byte {
	if !m.active() || len(body) == 0 {
		return body
	}
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return body
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		// Array, scalar, or JSON null: nothing to strip.
		return body
	}
	for f := range m.ignoreSet {
		delete(obj, f)
	}
	return canonicalJSON(obj)
}
