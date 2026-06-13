package recording

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func diffByField(d []diffEntry) map[string]diffEntry {
	m := make(map[string]diffEntry, len(d))
	for _, e := range d {
		m[e.Field] = e
	}
	return m
}

func TestNearestDiff_FieldKinds(t *testing.T) {
	c := New("")
	_ = c.Append(&Interaction{
		Method: "POST", Path: "/v1/chat/completions",
		RequestBody: json.RawMessage(`{"model":"gpt-4o","keep":"same","only_cassette":1}`),
	})
	nm := nearestDiff(c, "POST", "/v1/chat/completions",
		[]byte(`{"model":"gpt-4.1","keep":"same","only_request":2}`))
	if nm == nil {
		t.Fatal("expected a nearest match")
	}
	by := diffByField(nm.Diff)
	if e, ok := by["model"]; !ok || e.Kind != "changed" {
		t.Errorf("model should be changed, got %+v", e)
	}
	if e, ok := by["only_cassette"]; !ok || e.Kind != "missing_in_request" {
		t.Errorf("only_cassette should be missing_in_request, got %+v", e)
	}
	if e, ok := by["only_request"]; !ok || e.Kind != "extra_in_request" {
		t.Errorf("only_request should be extra_in_request, got %+v", e)
	}
	if _, ok := by["keep"]; ok {
		t.Error("equal field 'keep' must not appear in the diff")
	}
}

func TestNearestDiff_Bounding(t *testing.T) {
	// Build a cassette request and a drifted request that differ in 30 fields.
	cass := map[string]any{}
	req := map[string]any{}
	for i := 0; i < 30; i++ {
		cass[fmt.Sprintf("f%02d", i)] = "a"
		req[fmt.Sprintf("f%02d", i)] = "b"
	}
	cb, _ := json.Marshal(cass)
	rb, _ := json.Marshal(req)
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/p", RequestBody: cb})
	nm := nearestDiff(c, "POST", "/p", rb)
	if nm == nil {
		t.Fatal("expected nearest")
	}
	if len(nm.Diff) != maxDiffEntries+1 {
		t.Fatalf("diff len = %d, want %d (cap + sentinel)", len(nm.Diff), maxDiffEntries+1)
	}
	last := nm.Diff[len(nm.Diff)-1]
	if last.Kind != "truncated" || !strings.Contains(last.Field, "more fields omitted") {
		t.Errorf("last entry should be the truncation sentinel, got %+v", last)
	}
}

func TestNearestDiff_ValueTruncation(t *testing.T) {
	big := strings.Repeat("x", 500)
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/p",
		RequestBody: json.RawMessage(fmt.Sprintf(`{"prompt":%q}`, big))})
	nm := nearestDiff(c, "POST", "/p", []byte(`{"prompt":"short"}`))
	if nm == nil {
		t.Fatal("expected nearest")
	}
	by := diffByField(nm.Diff)
	cv := string(by["prompt"].CassetteValue)
	if len(cv) > maxDiffValueLen+8 { // a little slack for the JSON string quoting + ellipsis
		t.Errorf("cassette value not truncated: len=%d", len(cv))
	}
	if !strings.Contains(cv, "…") {
		t.Errorf("truncated value should carry an ellipsis, got %s", cv)
	}
	// And it must still be valid JSON (a quoted string).
	if !json.Valid(by["prompt"].CassetteValue) {
		t.Errorf("truncated value must remain valid JSON: %s", cv)
	}
}

func TestNearestDiff_SameMethodPathPreferred(t *testing.T) {
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/other", RequestBody: json.RawMessage(`{"model":"gpt-4o"}`)})
	_ = c.Append(&Interaction{Method: "POST", Path: "/v1/chat/completions", RequestBody: json.RawMessage(`{"model":"x"}`)})
	nm := nearestDiff(c, "POST", "/v1/chat/completions", []byte(`{"model":"y"}`))
	if nm == nil || nm.Path != "/v1/chat/completions" {
		t.Fatalf("nearest should be the same-path interaction, got %+v", nm)
	}
}

func TestNearestDiff_TieBreakEarliestInsertion(t *testing.T) {
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/p", Hash: "first0000000000", RequestBody: json.RawMessage(`{"a":1}`)})
	_ = c.Append(&Interaction{Method: "POST", Path: "/p", Hash: "second000000000", RequestBody: json.RawMessage(`{"a":2}`)})
	// Request matches neither; both score equally (0 matching of 1 union). Earliest wins.
	nm := nearestDiff(c, "POST", "/p", []byte(`{"a":3}`))
	if nm == nil || nm.Hash != "first0000000" {
		t.Fatalf("tie should pick earliest insertion, got %+v", nm)
	}
}

func TestNearestDiff_EmptyCassette(t *testing.T) {
	if nm := nearestDiff(New(""), "POST", "/p", []byte(`{"a":1}`)); nm != nil {
		t.Errorf("empty cassette must yield nil nearest, got %+v", nm)
	}
}

func TestNearestDiff_NonJSONRequestBody(t *testing.T) {
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/p", RequestBody: json.RawMessage(`{"a":1}`)})
	nm := nearestDiff(c, "POST", "/p", []byte(`this is not json`)) // must not panic
	if nm == nil {
		t.Fatal("expected nearest even for a non-JSON request")
	}
	if nm.Similarity != 0 {
		t.Errorf("non-JSON request vs JSON cassette should score 0, got %v", nm.Similarity)
	}
	if len(nm.Diff) != 1 || nm.Diff[0].Field != "(body)" {
		t.Errorf("non-object diff should be a single (body) entry, got %+v", nm.Diff)
	}
}

func TestNearestDiff_InteractionWithNilRequestBody(t *testing.T) {
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/p"}) // nil RequestBody
	nm := nearestDiff(c, "POST", "/p", []byte(`{"a":1}`))  // must not panic
	if nm == nil {
		t.Fatal("expected nearest")
	}
}

func TestNearestDiff_DiffOrdering(t *testing.T) {
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/p",
		RequestBody: json.RawMessage(`{"zchanged":"a","mmissing":1,"achanged":"a"}`)})
	nm := nearestDiff(c, "POST", "/p",
		[]byte(`{"zchanged":"b","achanged":"b","xextra":1}`))
	if nm == nil {
		t.Fatal("expected nearest")
	}
	var kinds, fields []string
	for _, e := range nm.Diff {
		kinds = append(kinds, e.Kind)
		fields = append(fields, e.Field)
	}
	// changed (alphabetical) → missing → extra.
	want := []string{"achanged", "zchanged", "mmissing", "xextra"}
	for i, f := range want {
		if i >= len(fields) || fields[i] != f {
			t.Fatalf("diff order = %v (kinds %v), want %v", fields, kinds, want)
		}
	}
}

func TestNearestDiff_NumbersNormalizedLikeMatcher(t *testing.T) {
	// The diff's equality must match the matcher (float64): 1 and 1.0 are equal,
	// so a field that differs only in numeric spelling is NOT a spurious diff.
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/p",
		RequestBody: json.RawMessage(`{"temperature":1.0,"model":"a"}`)})
	nm := nearestDiff(c, "POST", "/p", []byte(`{"temperature":1,"model":"b"}`))
	if nm == nil {
		t.Fatal("expected nearest")
	}
	by := diffByField(nm.Diff)
	if _, ok := by["temperature"]; ok {
		t.Errorf("temperature (1.0 vs 1) must NOT be a diff entry — it hashes equal; got %+v", nm.Diff)
	}
	if e, ok := by["model"]; !ok || e.Kind != "changed" {
		t.Errorf("only model should be 'changed', got %+v", nm.Diff)
	}
}

func TestNearestDiff_NoSameEndpointReturnsNil(t *testing.T) {
	// A miss on an endpoint with no recordings must not report a misleading
	// cross-endpoint nearest (e.g. similarity 1.0 against a different path).
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/v1/messages", RequestBody: json.RawMessage(`{"model":"a"}`)})
	if nm := nearestDiff(c, "GET", "/totally/different", []byte(`{"model":"a"}`)); nm != nil {
		t.Errorf("cross-endpoint miss must yield nil nearest, got %+v", nm)
	}
}
