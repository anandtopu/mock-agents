package engine

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

func makeTenantAgent(name, model, tenantID string) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: name, TenantID: tenantID},
		Spec: types.AgentSpec{
			Model: model,
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "default",
					Response: types.ScenarioResponse{Content: "from " + name},
				}},
			},
		},
	}
}

// --- Registry visibility ---

func TestAgentRegistry_GetForTenant_GlobalVisibleToAll(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("global-bot", "gpt-4o", ""))

	if got := r.GetForTenant("global-bot", ""); got == nil {
		t.Error("anonymous caller should see global agent")
	}
	if got := r.GetForTenant("global-bot", "ten_a"); got == nil {
		t.Error("tenant caller should also see global agent")
	}
}

func TestAgentRegistry_GetForTenant_ScopedHiddenFromOthers(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("acme-bot", "gpt-4o", "ten_acme"))

	if got := r.GetForTenant("acme-bot", "ten_acme"); got == nil {
		t.Error("owner tenant should see the agent")
	}
	if got := r.GetForTenant("acme-bot", "ten_other"); got != nil {
		t.Error("other tenant should NOT see the agent")
	}
	if got := r.GetForTenant("acme-bot", ""); got != nil {
		t.Error("anonymous caller should NOT see tenant-scoped agent")
	}
}

func TestAgentRegistry_GetByModelForTenant_PrefersOwner(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("global", "shared-model", ""))
	r.Register(makeTenantAgent("acme", "shared-model", "ten_acme"))

	// Tenant caller resolves to their own override.
	got := r.GetByModelForTenant("shared-model", "ten_acme")
	if got == nil || got.Metadata.Name != "acme" {
		t.Errorf("tenant resolution mismatch: %+v", got)
	}
	// Anonymous caller falls back to the global agent.
	got = r.GetByModelForTenant("shared-model", "")
	if got == nil || got.Metadata.Name != "global" {
		t.Errorf("anonymous resolution mismatch: %+v", got)
	}
	// A different tenant also gets the global one (not the acme override).
	got = r.GetByModelForTenant("shared-model", "ten_other")
	if got == nil || got.Metadata.Name != "global" {
		t.Errorf("other-tenant resolution mismatch: %+v", got)
	}
}

func TestAgentRegistry_ListForTenant_FiltersScoped(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeTenantAgent("zglobal", "g", ""))
	r.Register(makeTenantAgent("acme-x", "gpt-4o", "ten_acme"))
	r.Register(makeTenantAgent("acme-y", "claude", "ten_acme"))
	r.Register(makeTenantAgent("beta-z", "claude", "ten_beta"))

	acme := r.ListForTenant("ten_acme")
	names := make([]string, len(acme))
	for i, a := range acme {
		names[i] = a.Metadata.Name
	}
	want := []string{"acme-x", "acme-y", "zglobal"}
	if !equalStringSlices(names, want) {
		t.Errorf("acme view = %v, want %v", names, want)
	}

	anon := r.ListForTenant("")
	if len(anon) != 1 || anon[0].Metadata.Name != "zglobal" {
		t.Errorf("anonymous view = %v, want [zglobal]", anon)
	}

	beta := r.ListForTenant("ten_beta")
	betaNames := make([]string, len(beta))
	for i, a := range beta {
		betaNames[i] = a.Metadata.Name
	}
	if !equalStringSlices(betaNames, []string{"beta-z", "zglobal"}) {
		t.Errorf("beta view = %v", betaNames)
	}
}

// --- Engine integration ---

func newTestEngineForTenants(t *testing.T) *Engine {
	t.Helper()
	r := NewAgentRegistry()
	// Two global agents so the "single-agent default" fallback
	// cannot fire and mask a missed lookup. The tests that follow
	// rely on resolution failing cleanly for cross-tenant attempts.
	r.Register(makeTenantAgent("global-bot", "gpt-4o", ""))
	r.Register(makeTenantAgent("global-other", "gpt-4o-mini", ""))
	r.Register(makeTenantAgent("acme-bot", "claude-x", "ten_acme"))
	r.Register(makeTenantAgent("beta-bot", "claude-y", "ten_beta"))

	return NewEngine(r, state.NewMemoryStore(0), slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

func TestEngine_TenantScopedResolveByName(t *testing.T) {
	e := newTestEngineForTenants(t)

	ctx := WithTenantID(context.Background(), "ten_acme")
	resp, err := e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: "acme-bot",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("acme tenant should resolve own agent: %v", err)
	}
	if resp.AgentName != "acme-bot" {
		t.Errorf("got %q", resp.AgentName)
	}
}

func TestEngine_TenantCannotResolveOtherTenantAgent(t *testing.T) {
	e := newTestEngineForTenants(t)

	ctx := WithTenantID(context.Background(), "ten_acme")
	_, err := e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: "beta-bot", // belongs to ten_beta
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sess-2",
	})
	if err == nil {
		t.Fatal("expected ErrAgentNotFound for cross-tenant lookup")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v", err)
	}
}

func TestEngine_AnonymousCallerSeesOnlyGlobal(t *testing.T) {
	e := newTestEngineForTenants(t)

	ctx := context.Background()
	resp, err := e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: "global-bot",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sess-3",
	})
	if err != nil {
		t.Fatalf("anonymous should resolve global agent: %v", err)
	}
	if resp.AgentName != "global-bot" {
		t.Errorf("got %q", resp.AgentName)
	}

	// And cannot reach a tenant-scoped one.
	_, err = e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: "acme-bot",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "sess-4",
	})
	if err == nil {
		t.Fatal("anonymous should not resolve tenant-scoped agent")
	}
}

// --- TenantIDFromContext / WithTenantID round-trip ---

func TestTenantIDContextRoundTrip(t *testing.T) {
	if got := TenantIDFromContext(context.Background()); got != "" {
		t.Errorf("default = %q, want empty", got)
	}
	ctx := WithTenantID(context.Background(), "ten_xyz")
	if got := TenantIDFromContext(ctx); got != "ten_xyz" {
		t.Errorf("round trip = %q", got)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
