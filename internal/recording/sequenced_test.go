package recording

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const seqBody = `{"model":"gpt-4o","messages":[{"role":"user","content":"loop"}]}`

// seqCassette builds an in-memory cassette with N interactions all sharing the
// same request hash (body), each with a distinct response body resp-1..resp-N.
func seqCassette(t *testing.T, n int) *Cassette {
	t.Helper()
	c := New("")
	for i := 1; i <= n; i++ {
		if err := c.Append(&Interaction{
			Method:         "POST",
			Path:           "/v1/chat/completions",
			RequestBody:    json.RawMessage(seqBody),
			ResponseStatus: 200,
			ResponseBody:   json.RawMessage(`{"resp":` + itoa(i) + `}`),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	return c
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func replayOnce(t *testing.T, rp *Replay) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(seqBody))
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, req)
	return rec
}

func TestSequencedReplay_InOrder(t *testing.T) {
	rp := NewReplay(seqCassette(t, 3))
	for i := 1; i <= 3; i++ {
		rec := replayOnce(t, rp)
		if rec.Code != http.StatusOK {
			t.Fatalf("turn %d: status %d", i, rec.Code)
		}
		want := `{"resp":` + itoa(i) + `}`
		if rec.Body.String() != want {
			t.Errorf("turn %d: body = %q, want %q", i, rec.Body.String(), want)
		}
	}
}

func TestSequencedReplay_RepeatLast(t *testing.T) {
	rp := NewReplay(seqCassette(t, 2))
	want := []string{`{"resp":1}`, `{"resp":2}`, `{"resp":2}`, `{"resp":2}`}
	for i, w := range want {
		got := replayOnce(t, rp).Body.String()
		if got != w {
			t.Errorf("request %d: body = %q, want %q (last must repeat)", i+1, got, w)
		}
	}
}

func TestSequencedReplay_SingleInteractionUnchanged(t *testing.T) {
	rp := NewReplay(seqCassette(t, 1))
	for i := 0; i < 3; i++ {
		rec := replayOnce(t, rp)
		if rec.Code != http.StatusOK || rec.Body.String() != `{"resp":1}` {
			t.Fatalf("request %d: status %d body %q", i, rec.Code, rec.Body.String())
		}
	}
}

func TestSequencedReplay_StreamingInSequence(t *testing.T) {
	c := New("")
	_ = c.Append(&Interaction{Method: "POST", Path: "/v1/chat/completions",
		RequestBody: json.RawMessage(seqBody), ResponseStatus: 200,
		ResponseBody: json.RawMessage(`{"resp":1}`)})
	_ = c.Append(&Interaction{Method: "POST", Path: "/v1/chat/completions",
		RequestBody: json.RawMessage(seqBody), ResponseStatus: 200,
		Streaming: true, StreamEvents: []StreamEvent{{Data: "data: chunk-A\n\n"}, {Data: "data: [DONE]\n\n"}}})
	rp := NewReplay(c)

	r1 := replayOnce(t, rp)
	if r1.Header().Get("X-Mockagents-Replay") != "hit" || r1.Body.String() != `{"resp":1}` {
		t.Errorf("turn 1 (non-streaming): hdr=%q body=%q", r1.Header().Get("X-Mockagents-Replay"), r1.Body.String())
	}
	r2 := replayOnce(t, rp)
	if r2.Header().Get("X-Mockagents-Replay") != "hit-streaming" || !strings.Contains(r2.Body.String(), "chunk-A") {
		t.Errorf("turn 2 (streaming): hdr=%q body=%q", r2.Header().Get("X-Mockagents-Replay"), r2.Body.String())
	}
	// repeat-last: the streaming interaction again.
	r3 := replayOnce(t, rp)
	if !strings.Contains(r3.Body.String(), "chunk-A") {
		t.Errorf("turn 3 (repeat-last streaming): body=%q", r3.Body.String())
	}
}

func TestSequencedReplay_ConcurrentSameHash(t *testing.T) {
	rp := NewReplay(seqCassette(t, 5))
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := replayOnce(t, rp)
			if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
				t.Errorf("concurrent request: status %d empty=%v", rec.Code, rec.Body.Len() == 0)
				return
			}
			mu.Lock()
			seen[rec.Body.String()]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	// Each of the 5 interactions must have been served EXACTLY once — a
	// permutation of resp-1..resp-5, proving the cursor read+increment is
	// atomic under concurrency (no duplicates, no skips).
	for i := 1; i <= 5; i++ {
		want := `{"resp":` + itoa(i) + `}`
		if seen[want] != 1 {
			t.Errorf("interaction %s served %d times, want exactly 1 (seen=%v)", want, seen[want], seen)
		}
	}
}

func TestSequencedReplay_DirectConstructionNoPanic(t *testing.T) {
	// A directly-constructed Replay (not via NewReplay) has a nil cursor map;
	// next() must lazily initialize it rather than panic on the map write.
	rp := &Replay{Cassette: seqCassette(t, 1)}
	rec := replayOnce(t, rp)
	if rec.Code != http.StatusOK {
		t.Fatalf("direct-construction replay: status %d", rec.Code)
	}
}

func TestSequencedReplay_MissStill404(t *testing.T) {
	rp := NewReplay(New("")) // empty cassette
	rec := replayOnce(t, rp)
	if rec.Code != http.StatusNotFound {
		t.Errorf("miss should 404, got %d", rec.Code)
	}
}

func TestLookupSequence_OrderingAndDefensiveCopy(t *testing.T) {
	c := seqCassette(t, 3)
	hash := HashRequest("POST", "/v1/chat/completions", []byte(seqBody))
	seq := c.LookupSequence(hash)
	if len(seq) != 3 {
		t.Fatalf("LookupSequence len = %d, want 3", len(seq))
	}
	for i, it := range seq {
		want := `{"resp":` + itoa(i+1) + `}`
		if string(it.ResponseBody) != want {
			t.Errorf("seq[%d] = %q, want %q", i, it.ResponseBody, want)
		}
	}
	// Reassigning a slice element must not affect the cassette (slice-level
	// isolation; the *Interaction values themselves are shared read-only).
	seq[0] = nil
	if again := c.LookupSequence(hash); again[0] == nil {
		t.Error("LookupSequence returned a slice aliasing the cassette's own")
	}
	// Lookup still returns the FIRST interaction (backward compat).
	if first := c.Lookup(hash); first == nil || string(first.ResponseBody) != `{"resp":1}` {
		t.Errorf("Lookup backward-compat broken: %+v", first)
	}
}

func TestSequencedReplay_LoadFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seq.jsonl")
	// Write 3 lines sharing the same hash directly.
	c := New(path)
	for i := 1; i <= 3; i++ {
		_ = c.Append(&Interaction{Method: "POST", Path: "/v1/chat/completions",
			RequestBody: json.RawMessage(seqBody), ResponseStatus: 200,
			ResponseBody: json.RawMessage(`{"resp":` + itoa(i) + `}`)})
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cassette not written: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rp := NewReplay(loaded)
	for i := 1; i <= 3; i++ {
		got := replayOnce(t, rp).Body.String()
		want := `{"resp":` + itoa(i) + `}`
		if got != want {
			t.Errorf("loaded turn %d: body = %q, want %q", i, got, want)
		}
	}
}
