package recording

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Replay is an HTTP handler that serves previously recorded interactions
// from a Cassette. Unknown requests either fall through to Fallback (if
// set) or return 404.
type Replay struct {
	Cassette *Cassette
	// Fallback is invoked when no matching interaction is found. If nil,
	// replay returns a 404 with a descriptive error body.
	Fallback http.Handler
	// PreserveStreamDelays, when true, causes streaming replays to
	// honor the original DelayMs timestamps between chunks. Defaults
	// off so CI suites get deterministic fast replays; set to true
	// for demos where realistic pacing matters.
	PreserveStreamDelays bool
}

// NewReplay builds a Replay handler.
func NewReplay(c *Cassette) *Replay {
	return &Replay{Cassette: c}
}

// ServeHTTP looks up the incoming request in the cassette and, on a hit,
// writes the recorded status, headers and body back to the client.
func (rp *Replay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := DrainBody(r)
	if err != nil {
		http.Error(w, "reading request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	hash := HashRequest(r.Method, r.URL.Path, body)
	it := rp.Cassette.Lookup(hash)
	if it == nil {
		if rp.Fallback != nil {
			// Reset the body so the fallback can read it.
			r.Body = io.NopCloser(bytes.NewReader(body))
			rp.Fallback.ServeHTTP(w, r)
			return
		}
		http.Error(w,
			fmt.Sprintf("no cassette match for %s %s (hash=%s)", r.Method, r.URL.Path, hash[:12]),
			http.StatusNotFound)
		return
	}

	// Streaming hit: replay captured SSE chunks in order, optionally
	// honoring the original inter-chunk delays.
	if it.Streaming {
		rp.serveStreaming(w, it)
		return
	}

	for k, v := range it.ResponseHeaders {
		w.Header().Set(k, v)
	}
	w.Header().Set("X-Mockagents-Replay", "hit")
	status := it.ResponseStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(it.ResponseBody)
}

// serveStreaming writes a captured SSE interaction back to the client,
// flushing after every chunk so downstream consumers see the same
// incremental arrivals they would from a real LLM server.
func (rp *Replay) serveStreaming(w http.ResponseWriter, it *Interaction) {
	for k, v := range it.ResponseHeaders {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.Header().Set("X-Mockagents-Replay", "hit-streaming")
	status := it.ResponseStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	start := time.Now()
	for _, ev := range it.StreamEvents {
		if rp.PreserveStreamDelays && ev.DelayMs > 0 {
			target := start.Add(time.Duration(ev.DelayMs) * time.Millisecond)
			if d := time.Until(target); d > 0 {
				time.Sleep(d)
			}
		}
		if _, err := w.Write([]byte(ev.Data)); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}
