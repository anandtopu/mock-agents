package recording

import "testing"

func TestMatcherKey_IgnoredFieldsAreInvisible(t *testing.T) {
	m := NewMatcher([]string{"temperature", "seed"})
	a := m.Key("POST", "/v1/chat/completions", []byte(`{"model":"gpt-4o","temperature":0.2,"seed":1}`))
	b := m.Key("POST", "/v1/chat/completions", []byte(`{"model":"gpt-4o","temperature":0.9,"seed":42}`))
	if a != b {
		t.Errorf("keys differ on ignored fields only:\n a=%s\n b=%s", a, b)
	}
}

func TestMatcherKey_UnignoredFieldsStillDiffer(t *testing.T) {
	m := NewMatcher([]string{"temperature"})
	a := m.Key("POST", "/v1/chat/completions", []byte(`{"model":"gpt-4o","temperature":0.2}`))
	b := m.Key("POST", "/v1/chat/completions", []byte(`{"model":"gpt-4.1","temperature":0.2}`))
	if a == b {
		t.Error("keys must differ when a non-ignored field (model) changes")
	}
}

func TestMatcherKey_NonObjectBodyUnchanged(t *testing.T) {
	m := NewMatcher([]string{"temperature"})
	arr := []byte(`[1,2,3]`)
	if got, want := m.Key("POST", "/p", arr), HashRequest("POST", "/p", arr); got != want {
		t.Error("array body must hash like the raw body (no top-level fields to strip)")
	}
}

func TestMatcherKey_NonJSONBodyUnchanged(t *testing.T) {
	m := NewMatcher([]string{"temperature"})
	raw := []byte(`not json at all`)
	if got, want := m.Key("POST", "/p", raw), HashRequest("POST", "/p", raw); got != want {
		t.Error("non-JSON body must pass through unchanged")
	}
}

func TestMatcherKey_NullTopLevelJSON(t *testing.T) {
	m := NewMatcher([]string{"temperature"})
	raw := []byte(`null`)
	if got, want := m.Key("POST", "/p", raw), HashRequest("POST", "/p", raw); got != want {
		t.Error("JSON null body must pass through unchanged")
	}
}

func TestMatcherKey_EmptyBodyStable(t *testing.T) {
	m := NewMatcher([]string{"temperature"})
	if got, want := m.Key("POST", "/p", nil), HashRequest("POST", "/p", nil); got != want {
		t.Error("empty body key must equal HashRequest of empty body")
	}
}

func TestMatcherKey_InactiveMatcherEqualsHashRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","temperature":0.2}`)
	want := HashRequest("POST", "/p", body)
	for name, m := range map[string]*Matcher{
		"nil-slice":   NewMatcher(nil),
		"empty-slice": NewMatcher([]string{}),
	} {
		if got := m.Key("POST", "/p", body); got != want {
			t.Errorf("%s: inactive matcher Key must equal HashRequest", name)
		}
	}
}

func TestMatcherActive(t *testing.T) {
	var nilM *Matcher
	if nilM.active() {
		t.Error("nil matcher must be inactive")
	}
	if NewMatcher(nil).active() {
		t.Error("empty matcher must be inactive")
	}
	if !NewMatcher([]string{"x"}).active() {
		t.Error("non-empty matcher must be active")
	}
}
