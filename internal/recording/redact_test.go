package recording

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// --- Redactor unit tests ---

func TestNewRedactor_BadPattern(t *testing.T) {
	if _, err := NewRedactor([]string{"[invalid"}); err == nil {
		t.Error("expected error compiling an invalid regexp")
	}
}

func TestRedactor_DefaultKeysMasked(t *testing.T) {
	r, _ := NewRedactor(nil)
	it := &Interaction{
		RequestBody:  json.RawMessage(`{"api_key":"sk-req-secret"}`),
		ResponseBody: json.RawMessage(`{"echo":"sk-resp-secret","auth":"Bearer tok-xyz"}`),
	}
	r.Apply(it)
	if strings.Contains(string(it.RequestBody), "req-secret") {
		t.Errorf("request key not masked: %s", it.RequestBody)
	}
	if strings.Contains(string(it.ResponseBody), "resp-secret") || strings.Contains(string(it.ResponseBody), "tok-xyz") {
		t.Errorf("response secrets not masked: %s", it.ResponseBody)
	}
	if !json.Valid(it.RequestBody) || !json.Valid(it.ResponseBody) {
		t.Error("redacted bodies must remain valid JSON")
	}
}

func TestRedactor_MultipleKeys(t *testing.T) {
	r, _ := NewRedactor(nil)
	it := &Interaction{RequestBody: json.RawMessage(`{"a":"sk-aaa","b":"sk-bbb"}`)}
	r.Apply(it)
	if strings.Contains(string(it.RequestBody), "aaa") || strings.Contains(string(it.RequestBody), "bbb") {
		t.Errorf("both keys must be masked: %s", it.RequestBody)
	}
}

func TestRedactor_CustomPattern(t *testing.T) {
	r, err := NewRedactor([]string{`ssn-\d+`})
	if err != nil {
		t.Fatal(err)
	}
	it := &Interaction{RequestBody: json.RawMessage(`{"id":"ssn-123456","key":"sk-abc"}`)}
	r.Apply(it)
	if strings.Contains(string(it.RequestBody), "ssn-123456") {
		t.Errorf("custom pattern not applied: %s", it.RequestBody)
	}
	if strings.Contains(string(it.RequestBody), "abc") {
		t.Errorf("default masking should still run alongside custom: %s", it.RequestBody)
	}
}

func TestRedactor_StreamEventsRedacted(t *testing.T) {
	r, _ := NewRedactor(nil)
	it := &Interaction{
		Streaming:    true,
		StreamEvents: []StreamEvent{{Data: "data: {\"key\":\"sk-stream-secret\"}\n\n"}},
	}
	r.Apply(it)
	if strings.Contains(it.StreamEvents[0].Data, "stream-secret") {
		t.Errorf("stream event not redacted: %s", it.StreamEvents[0].Data)
	}
}

func TestRedactor_HeadersRedacted(t *testing.T) {
	r, _ := NewRedactor(nil)
	it := &Interaction{RequestHeaders: map[string]string{"X-Thing": "sk-hdr-secret"}}
	r.Apply(it)
	if strings.Contains(it.RequestHeaders["X-Thing"], "hdr-secret") {
		t.Errorf("header value not redacted: %s", it.RequestHeaders["X-Thing"])
	}
}

func TestRedactor_NilSafe(t *testing.T) {
	var r *Redactor
	r.Apply(&Interaction{}) // must not panic
	r2, _ := NewRedactor(nil)
	r2.Apply(nil) // must not panic
}

func TestRedactor_CustomPatternCannotBreakStructure(t *testing.T) {
	// A custom pattern that matches the quote char is applied to string VALUES
	// only, so it can never corrupt JSON framing. The body must stay valid JSON
	// and the default key must still be masked.
	r, _ := NewRedactor([]string{`"`})
	it := &Interaction{RequestBody: json.RawMessage(`{"k":"sk-abc"}`)}
	r.Apply(it)
	if !json.Valid(it.RequestBody) {
		t.Errorf("body must stay valid JSON: %s", it.RequestBody)
	}
	if strings.Contains(string(it.RequestBody), "abc") {
		t.Errorf("key must still be masked: %s", it.RequestBody)
	}
}

func TestRedactor_CustomPatternMasksPrefixlessValueNoLeak(t *testing.T) {
	// The previously-leaking case: a prefixless secret only a custom pattern can
	// catch must be masked, and the body must stay valid JSON with its structure
	// intact (no key dropped, no re-leak via a structure-break fallback).
	r, err := NewRedactor([]string{`session=[A-Za-z0-9]+`})
	if err != nil {
		t.Fatal(err)
	}
	it := &Interaction{RequestBody: json.RawMessage(`{"auth":"session=DEADBEEFCAFE","ok":true}`)}
	r.Apply(it)
	if strings.Contains(string(it.RequestBody), "DEADBEEFCAFE") {
		t.Errorf("custom-targeted secret leaked into cassette: %s", it.RequestBody)
	}
	if !json.Valid(it.RequestBody) {
		t.Errorf("body must stay valid JSON: %s", it.RequestBody)
	}
	var m map[string]any
	if err := json.Unmarshal(it.RequestBody, &m); err != nil {
		t.Fatalf("redacted body not decodable: %v", err)
	}
	if _, ok := m["auth"]; !ok {
		t.Errorf("redaction must not drop/rename keys: %s", it.RequestBody)
	}
	if m["ok"] != true {
		t.Errorf("non-string values must be preserved: %s", it.RequestBody)
	}
}

func TestRedactor_BuiltinSecretFormats(t *testing.T) {
	r, _ := NewRedactor(nil)
	cases := map[string]string{
		"aws":    `{"k":"AKIAIOSFODNN7EXAMPLE"}`,
		"github": `{"k":"ghp_1234567890abcdefghijklmnopqrstuvwxyz"}`,
		"slack":  `{"k":"xoxb-12345-67890-abcdefghijklmnop"}`,
		"google": `{"k":"AIzaSyA1234567890abcdefghijklmnopqrstuv"}`,
		"jwt":    `{"k":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"}`,
	}
	for name, body := range cases {
		it := &Interaction{ResponseBody: json.RawMessage(body)}
		r.Apply(it)
		if !json.Valid(it.ResponseBody) {
			t.Errorf("%s: body must stay valid JSON: %s", name, it.ResponseBody)
		}
		if !strings.Contains(string(it.ResponseBody), "***") {
			t.Errorf("%s: secret format not masked: %s", name, it.ResponseBody)
		}
	}
}

func TestRedactor_PreservesNumbersAndStructure(t *testing.T) {
	// Large ints (token ids) must survive the decode/encode round-trip exactly,
	// and only string values are touched.
	r, _ := NewRedactor(nil)
	it := &Interaction{ResponseBody: json.RawMessage(`{"id":9007199254740993,"key":"sk-secret","ok":false}`)}
	r.Apply(it)
	if !strings.Contains(string(it.ResponseBody), "9007199254740993") {
		t.Errorf("large int lost precision: %s", it.ResponseBody)
	}
	if strings.Contains(string(it.ResponseBody), "secret") {
		t.Errorf("key value not masked: %s", it.ResponseBody)
	}
	if !strings.Contains(string(it.ResponseBody), "false") {
		t.Errorf("bool value not preserved: %s", it.ResponseBody)
	}
}

func TestRedactor_NoMatchReturnsVerbatim(t *testing.T) {
	// A secret-free body must come back byte-for-byte (no key reordering / churn).
	r, _ := NewRedactor(nil)
	const body = `{"model":"gpt-4o","b":2,"a":1}`
	it := &Interaction{RequestBody: json.RawMessage(body)}
	r.Apply(it)
	if string(it.RequestBody) != body {
		t.Errorf("secret-free body must be verbatim, got: %s", it.RequestBody)
	}
}

func TestRedactor_StreamCustomPatternPreservesFraming(t *testing.T) {
	// A structure-breaking custom pattern must not corrupt an SSE frame's JSON.
	r, _ := NewRedactor([]string{`"`})
	it := &Interaction{
		Streaming:    true,
		StreamEvents: []StreamEvent{{Data: "data: {\"key\":\"sk-stream-secret\"}\n\n"}},
	}
	r.Apply(it)
	got := it.StreamEvents[0].Data
	if strings.Contains(got, "stream-secret") {
		t.Errorf("stream secret leaked: %q", got)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(got, "data: "), "\n\n")
	if !json.Valid([]byte(payload)) {
		t.Errorf("custom pattern corrupted the SSE frame's JSON: %q", got)
	}
}

// --- proxy integration ---

func TestProxyRedactsBodyButClientGetsRealKey(t *testing.T) {
	const secret = "sk-live-supersecret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"echo":"` + secret + `"}`))
	}))
	defer upstream.Close()

	path := filepath.Join(t.TempDir(), "cass.jsonl")
	cass := New(path)
	proxy, _ := NewProxy(upstream.URL, cass)
	proxy.Redactor, _ = NewRedactor(nil)
	front := httptest.NewServer(proxy)
	defer front.Close()

	reqBody := `{"model":"gpt-4o","api_key":"` + secret + `"}`
	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	clientBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// The client must receive the REAL upstream response (un-redacted).
	if !bytes.Contains(clientBody, []byte(secret)) {
		t.Errorf("client should get the un-redacted upstream body, got: %s", clientBody)
	}
	// The cassette (request + response bodies) must NOT contain the secret.
	it := cass.All()[0]
	if bytes.Contains(it.RequestBody, []byte(secret)) {
		t.Errorf("cassette request body leaked the secret: %s", it.RequestBody)
	}
	if bytes.Contains(it.ResponseBody, []byte(secret)) {
		t.Errorf("cassette response body leaked the secret: %s", it.ResponseBody)
	}
}

func TestProxyHashPreservedAfterRedaction(t *testing.T) {
	const secret = "sk-req-keytohash"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cass := New("")
	proxy, _ := NewProxy(upstream.URL, cass)
	proxy.Redactor, _ = NewRedactor(nil)
	front := httptest.NewServer(proxy)
	defer front.Close()

	// Record a request whose body contains a key.
	reqBody := `{"model":"gpt-4o","api_key":"` + secret + `"}`
	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Replay must still match when the client sends the ORIGINAL (un-redacted)
	// request — the cassette hash was computed before redaction.
	rp := NewReplay(cass)
	rrec := httptest.NewRecorder()
	rreq := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	rp.ServeHTTP(rrec, rreq)
	if rrec.Code != http.StatusOK {
		t.Fatalf("replay of original request missed (hash computed from redacted body?): status %d", rrec.Code)
	}
}

func TestProxyNilRedactorRecordsVerbatim(t *testing.T) {
	const secret = "sk-not-redacted"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"echo":"` + secret + `"}`))
	}))
	defer upstream.Close()

	cass := New("")
	proxy, _ := NewProxy(upstream.URL, cass) // no Redactor
	front := httptest.NewServer(proxy)
	defer front.Close()

	resp, _ := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"x":1}`))
	resp.Body.Close()
	if !bytes.Contains(cass.All()[0].ResponseBody, []byte(secret)) {
		t.Error("without a Redactor, bodies must be recorded verbatim")
	}
}
