package mockagents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// newMcpTestServer spins up a canned httptest.Server that serves
// GET /mcp/events with the supplied SSE frames and collects every
// POST /mcp/response body for later inspection.
func newMcpTestServer(t *testing.T, frames []string) (*httptest.Server, *[]string) {
	t.Helper()
	var (
		mu        sync.Mutex
		responses []string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		for _, frame := range frames {
			_, _ = fmt.Fprint(w, frame+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("POST /mcp/response", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		responses = append(responses, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &responses
}

func TestMcpClient_ParsesRequestFrame(t *testing.T) {
	frames := []string{
		`event: request
data: {"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{"prompt":"hi"}}`,
	}
	srv, _ := newMcpTestServer(t, frames)

	client := NewMcpClient(McpClientOptions{BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer stream.Close()

	if !stream.Next() {
		t.Fatalf("Next returned false: %v", stream.Err())
	}
	ev := stream.Value()
	if !ev.IsRequest() {
		t.Errorf("expected request, kind = %q", ev.Kind)
	}
	if ev.Payload.Method != "sampling/createMessage" {
		t.Errorf("method = %q", ev.Payload.Method)
	}
	if ev.Params()["prompt"] != "hi" {
		t.Errorf("params = %+v", ev.Params())
	}
}

func TestMcpClient_SkipsHeartbeatsAndMalformed(t *testing.T) {
	frames := []string{
		`:heartbeat`,
		`event: request
data: not-json`,
		`event: request
data: {"jsonrpc":"2.0","id":2,"method":"roots/list"}`,
	}
	srv, _ := newMcpTestServer(t, frames)

	client := NewMcpClient(McpClientOptions{BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	var methods []string
	for stream.Next() {
		methods = append(methods, stream.Value().Payload.Method)
	}
	if len(methods) != 1 || methods[0] != "roots/list" {
		t.Errorf("methods = %v", methods)
	}
}

func TestMcpClient_SendResponseResult(t *testing.T) {
	srv, recorded := newMcpTestServer(t, nil)
	client := NewMcpClient(McpClientOptions{BaseURL: srv.URL})

	err := client.SendResponse(context.Background(),
		json.RawMessage("7"),
		map[string]any{"text": "pong"},
		nil,
	)
	if err != nil {
		t.Fatalf("SendResponse: %v", err)
	}
	if len(*recorded) != 1 {
		t.Fatalf("recorded = %d", len(*recorded))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte((*recorded)[0]), &body); err != nil {
		t.Fatal(err)
	}
	if body["result"].(map[string]any)["text"] != "pong" {
		t.Errorf("body = %+v", body)
	}
}

func TestMcpClient_SendResponseError(t *testing.T) {
	srv, recorded := newMcpTestServer(t, nil)
	client := NewMcpClient(McpClientOptions{BaseURL: srv.URL})

	err := client.SendResponse(context.Background(),
		json.RawMessage(`"abc"`),
		nil,
		&JSONRPCError{Code: -32603, Message: "boom"},
	)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte((*recorded)[0]), &body)
	errObj := body["error"].(map[string]any)
	if errObj["code"].(float64) != -32603 {
		t.Errorf("code = %v", errObj["code"])
	}
}

func TestMcpClient_SendResponseRequiresExactlyOneOf(t *testing.T) {
	client := NewMcpClient(McpClientOptions{BaseURL: "http://nowhere"})
	err := client.SendResponse(context.Background(), json.RawMessage("1"), nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	err = client.SendResponse(context.Background(), json.RawMessage("1"),
		map[string]any{}, &JSONRPCError{Code: 1, Message: "x"})
	if err == nil {
		t.Fatal("expected error for both supplied")
	}
}

func TestMcpClient_DispatchRequestRoutesToHandler(t *testing.T) {
	srv, recorded := newMcpTestServer(t, nil)
	client := NewMcpClient(McpClientOptions{BaseURL: srv.URL})

	event := &McpEvent{
		Kind: "request",
		Payload: JSONRPCEnvelope{
			JSONRPC: "2.0",
			ID:      json.RawMessage("42"),
			Method:  "sampling/createMessage",
			Params:  map[string]any{"prompt": "hi"},
		},
	}

	var seen map[string]any
	handlers := map[string]McpRequestHandler{
		"sampling/createMessage": func(_ context.Context, params map[string]any) (map[string]any, error) {
			seen = params
			return map[string]any{"text": "ok"}, nil
		},
	}
	result, err := client.DispatchRequest(context.Background(), event, handlers)
	if err != nil {
		t.Fatal(err)
	}
	if result["text"] != "ok" || seen["prompt"] != "hi" {
		t.Errorf("result=%+v seen=%+v", result, seen)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte((*recorded)[0]), &body)
	if body["result"].(map[string]any)["text"] != "ok" {
		t.Errorf("posted body = %+v", body)
	}
}

func TestMcpClient_DispatchRequestUnknownMethod(t *testing.T) {
	srv, recorded := newMcpTestServer(t, nil)
	client := NewMcpClient(McpClientOptions{BaseURL: srv.URL})
	event := &McpEvent{
		Kind: "request",
		Payload: JSONRPCEnvelope{
			ID:     json.RawMessage("1"),
			Method: "unknown/method",
		},
	}
	_, err := client.DispatchRequest(context.Background(), event, map[string]McpRequestHandler{})
	if err == nil {
		t.Fatal("expected error")
	}
	// The client should have posted a -32601 error.
	var body map[string]any
	_ = json.Unmarshal([]byte((*recorded)[0]), &body)
	errObj := body["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 {
		t.Errorf("code = %v", errObj["code"])
	}
}

func TestMcpClient_DispatchRequestHandlerErrorPostsInternal(t *testing.T) {
	srv, recorded := newMcpTestServer(t, nil)
	client := NewMcpClient(McpClientOptions{BaseURL: srv.URL})
	event := &McpEvent{
		Kind: "request",
		Payload: JSONRPCEnvelope{
			ID:     json.RawMessage("9"),
			Method: "sampling/createMessage",
		},
	}
	handlers := map[string]McpRequestHandler{
		"sampling/createMessage": func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, errors.New("kaboom")
		},
	}
	_, err := client.DispatchRequest(context.Background(), event, handlers)
	if err == nil || err.Error() != "kaboom" {
		t.Errorf("err = %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte((*recorded)[0]), &body)
	errObj := body["error"].(map[string]any)
	if errObj["code"].(float64) != -32603 {
		t.Errorf("code = %v", errObj["code"])
	}
	if errObj["message"] != "kaboom" {
		t.Errorf("message = %v", errObj["message"])
	}
}

func TestMcpClient_DispatchRequestRejectsNotification(t *testing.T) {
	client := NewMcpClient(McpClientOptions{BaseURL: "http://nowhere"})
	event := &McpEvent{
		Kind: "notification",
		Payload: JSONRPCEnvelope{
			Method: "notifications/tools/list_changed",
		},
	}
	_, err := client.DispatchRequest(context.Background(), event, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMcpEvent_IsRequestRequiresID(t *testing.T) {
	withID := &McpEvent{Kind: "request", Payload: JSONRPCEnvelope{ID: json.RawMessage("1")}}
	if !withID.IsRequest() {
		t.Error("event with id should be a request")
	}
	withoutID := &McpEvent{Kind: "request", Payload: JSONRPCEnvelope{Method: "m"}}
	if withoutID.IsRequest() {
		t.Error("event without id should not be a request")
	}
	notif := &McpEvent{Kind: "notification", Payload: JSONRPCEnvelope{Method: "m"}}
	if notif.IsRequest() {
		t.Error("notification should not be a request")
	}
}

func TestMcpEvent_ParamsAlwaysReturnsMap(t *testing.T) {
	ev := &McpEvent{Payload: JSONRPCEnvelope{Method: "m"}}
	if ev.Params() == nil {
		t.Error("Params() must be non-nil")
	}
	if len(ev.Params()) != 0 {
		t.Errorf("Params() = %+v", ev.Params())
	}
}
