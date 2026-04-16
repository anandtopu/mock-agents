package engine

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestRequestMetaFromContext_Nil(t *testing.T) {
	if got := RequestMetaFromContext(context.Background()); got != nil {
		t.Fatalf("expected nil meta on empty ctx, got %+v", got)
	}
}

func TestWithRequestMeta_RoundTrip(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r2, meta := WithRequestMeta(r)

	if meta == nil {
		t.Fatal("WithRequestMeta returned nil meta")
	}
	if r2 == r {
		t.Fatal("WithRequestMeta should return a new request")
	}

	meta.AgentName = "echo-agent"
	meta.Model = "gpt-4o"

	got := RequestMetaFromContext(r2.Context())
	if got == nil {
		t.Fatal("expected meta in context, got nil")
	}
	if got.AgentName != "echo-agent" || got.Model != "gpt-4o" {
		t.Fatalf("meta round-trip failed: %+v", got)
	}
	if got != meta {
		t.Fatal("returned pointer should be identical to stored pointer")
	}
}
