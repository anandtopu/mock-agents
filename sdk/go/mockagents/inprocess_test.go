package mockagents

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeTempAgent drops a minimal agent YAML into a temp dir and returns
// the directory path. The agent replies with "pong" on any message.
func writeTempAgent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	yaml := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: ping-agent
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        match:
          default: true
        response:
          content: "pong"
`
	if err := os.WriteFile(filepath.Join(dir, "ping.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInProcessClientChat(t *testing.T) {
	dir := writeTempAgent(t)
	client, err := NewInProcessClient(InProcessOptions{AgentsDir: dir})
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	defer client.Close()

	if client.BaseURL() == "" {
		t.Error("expected non-empty BaseURL")
	}

	resp, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: "user", Content: "ping"}},
		ChatOptions{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "pong" {
		t.Errorf("content = %q, want pong", resp.Content)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestInProcessClientHealth(t *testing.T) {
	dir := writeTempAgent(t)
	client, err := NewInProcessClient(InProcessOptions{AgentsDir: dir})
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	defer client.Close()

	h, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h["mode"] != "in-process" {
		t.Errorf("health = %+v", h)
	}
}

func TestInProcessClientMissingDir(t *testing.T) {
	_, err := NewInProcessClient(InProcessOptions{AgentsDir: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestInProcessClientRequiresDir(t *testing.T) {
	_, err := NewInProcessClient(InProcessOptions{})
	if err == nil {
		t.Fatal("expected error for empty AgentsDir")
	}
}

func TestInProcessClientEmptyDirFails(t *testing.T) {
	dir := t.TempDir()
	_, err := NewInProcessClient(InProcessOptions{AgentsDir: dir})
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}
