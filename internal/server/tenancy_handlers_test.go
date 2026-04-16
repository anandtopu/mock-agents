package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// newRotateTestStore opens an isolated tenancy store under t.TempDir()
// and returns it plus a cleanup hook.
func newRotateTestStore(t *testing.T) *tenancy.SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := tenancy.NewSQLiteStore(filepath.Join(dir, "tenancy.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestTenancyHandlers_RotateAPIKey(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateAPIKey(ctx, tenant.ID, "ci", tenancy.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}

	// The handler records audit events but the in-memory audit
	// recorder with a nil store is a no-op — that's the same shape
	// server.New uses when AuditStore is unset.
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/keys/{id}/rotate", h.RotateAPIKey)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/keys/"+created.Key.ID+"/rotate",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out tenancy.NewAPIKeyResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Key.ID != created.Key.ID {
		t.Errorf("id changed: %q", out.Key.ID)
	}
	if out.Plaintext == "" || out.Plaintext == created.Plaintext {
		t.Errorf("plaintext = %q (old %q)", out.Plaintext, created.Plaintext)
	}
	// The old plaintext must no longer resolve, and the new one must.
	if _, err := store.Resolve(ctx, created.Plaintext); err == nil {
		t.Error("old plaintext still resolves")
	}
	if _, err := store.Resolve(ctx, out.Plaintext); err != nil {
		t.Errorf("new plaintext fails to resolve: %v", err)
	}
}

func TestTenancyHandlers_RotateAPIKey_NotFound(t *testing.T) {
	store := newRotateTestStore(t)
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/keys/{id}/rotate", h.RotateAPIKey)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/keys/key_bogus/rotate",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestTenancyHandlers_RotateMyAPIKey covers the self-rotation path.
// The handler reads the Principal from the request context (set by
// the auth middleware), so the test injects one manually via
// tenancy.WithPrincipal and asserts the round-trip: the old
// plaintext stops resolving, the new one resolves to the same key
// id, and the response body carries the fresh secret.
func TestTenancyHandlers_RotateMyAPIKey(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateAPIKey(ctx, tenant.ID, "self-rot", tenancy.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}

	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	// Wrap the handler with a tiny middleware that injects the
	// principal — mirrors what tenancy.AuthMiddleware does in
	// production without requiring the full auth chain.
	inject := func(next http.Handler) http.Handler {
		principal := &tenancy.Principal{
			TenantID: tenant.ID,
			KeyID:    created.Key.ID,
			Role:     tenancy.RoleViewer,
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := tenancy.WithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	srv := httptest.NewServer(inject(http.HandlerFunc(h.RotateMyAPIKey)))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out tenancy.NewAPIKeyResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Key.ID != created.Key.ID {
		t.Errorf("id changed: %q", out.Key.ID)
	}
	if out.Plaintext == "" || out.Plaintext == created.Plaintext {
		t.Errorf("plaintext unchanged: %q", out.Plaintext)
	}
	// Old plaintext must no longer resolve.
	if _, err := store.Resolve(ctx, created.Plaintext); err == nil {
		t.Error("old plaintext still resolves")
	}
	// New plaintext must resolve to the same key id + role.
	p, err := store.Resolve(ctx, out.Plaintext)
	if err != nil {
		t.Fatalf("new plaintext does not resolve: %v", err)
	}
	if p.KeyID != created.Key.ID {
		t.Errorf("key id changed on principal: %q", p.KeyID)
	}
	if p.Role != tenancy.RoleViewer {
		t.Errorf("role changed: %q", p.Role)
	}
}

// TestTenancyHandlers_RotateMyAPIKey_Unauthenticated covers the
// defensive 401 path: without a Principal on the context, the
// handler must refuse rather than blow up or read a nil key id.
func TestTenancyHandlers_BulkRotateTenantKeys(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := store.CreateAPIKey(ctx, tenant.ID, "ci-bot", tenancy.RoleEditor)
	b, _ := store.CreateAPIKey(ctx, tenant.ID, "viewer-bot", tenancy.RoleViewer)

	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants/{id}/keys/rotate", h.BulkRotateTenantKeys)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/tenants/"+tenant.ID+"/keys/rotate",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out BulkRotateResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
	if len(out.Results) != 2 {
		t.Fatalf("results = %d", len(out.Results))
	}
	// Both old plaintexts must be dead; both new plaintexts must
	// resolve via the store.
	if _, err := store.Resolve(ctx, a.Plaintext); err == nil {
		t.Error("old a plaintext still resolves")
	}
	if _, err := store.Resolve(ctx, b.Plaintext); err == nil {
		t.Error("old b plaintext still resolves")
	}
	for _, r := range out.Results {
		if _, err := store.Resolve(ctx, r.Plaintext); err != nil {
			t.Errorf("new plaintext for %q fails to resolve: %v", r.Key.Name, err)
		}
	}
}

func TestTenancyHandlers_BulkRotateTenantKeys_ExceptSelf(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "acme")
	admin, _ := store.CreateAPIKey(ctx, tenant.ID, "admin-self", tenancy.RoleAdmin)
	other, _ := store.CreateAPIKey(ctx, tenant.ID, "ci-bot", tenancy.RoleEditor)

	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	// Inject the admin as the caller's Principal so ?except=self
	// can resolve their key id.
	inject := func(next http.Handler) http.Handler {
		principal := &tenancy.Principal{
			TenantID: tenant.ID,
			KeyID:    admin.Key.ID,
			Role:     tenancy.RoleAdmin,
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := tenancy.WithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/tenants/{id}/keys/rotate", inject(http.HandlerFunc(h.BulkRotateTenantKeys)))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/tenants/"+tenant.ID+"/keys/rotate?except=self",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out BulkRotateResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	// Only the other key should be rotated, not the admin's own.
	if out.Count != 1 {
		t.Errorf("count = %d, want 1", out.Count)
	}
	// Admin's old plaintext must still resolve (excluded).
	if _, err := store.Resolve(ctx, admin.Plaintext); err != nil {
		t.Errorf("admin key should still resolve: %v", err)
	}
	// Other's old plaintext must be dead.
	if _, err := store.Resolve(ctx, other.Plaintext); err == nil {
		t.Error("other key should have been rotated")
	}
}

func TestTenancyHandlers_BulkRotateTenantKeys_UnknownTenant(t *testing.T) {
	store := newRotateTestStore(t)
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants/{id}/keys/rotate", h.BulkRotateTenantKeys)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/tenants/ten_bogus/keys/rotate",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTenancyHandlers_RotateMyAPIKey_Unauthenticated(t *testing.T) {
	store := newRotateTestStore(t)
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	srv := httptest.NewServer(http.HandlerFunc(h.RotateMyAPIKey))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// TestTenancyHandlers_BurnMyAPIKey exercises the rotate-and-burn
// path: the handler must rotate the caller's key (so the old
// plaintext dies) AND return 204 with an empty body (so the new
// plaintext never travels back over the wire). This is the
// "confirmed compromise" emergency response.
func TestTenancyHandlers_BurnMyAPIKey(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateAPIKey(ctx, tenant.ID, "self-burn", tenancy.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}

	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	inject := func(next http.Handler) http.Handler {
		principal := &tenancy.Principal{
			TenantID: tenant.ID,
			KeyID:    created.Key.ID,
			Role:     tenancy.RoleEditor,
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := tenancy.WithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	srv := httptest.NewServer(inject(http.HandlerFunc(h.BurnMyAPIKey)))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("body should be empty, got %q", string(body))
	}

	// Critical assertion: the server side DID rotate. The old
	// plaintext must no longer resolve, and a fresh call to the
	// store Resolve path with the old plaintext must fail.
	if _, err := store.Resolve(ctx, created.Plaintext); err == nil {
		t.Error("burn did not invalidate the old plaintext")
	}
	// The new key's row still exists (burn ≠ delete) — we prove
	// this by listing the tenant's keys and confirming the
	// single key id is preserved.
	keys, err := store.ListAPIKeys(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("tenant has %d keys after burn, want 1", len(keys))
	}
	if keys[0].ID != created.Key.ID {
		t.Errorf("key id changed: %q -> %q", created.Key.ID, keys[0].ID)
	}
	if keys[0].Prefix == created.Key.Prefix {
		t.Error("prefix unchanged — rotation did not happen")
	}
}

func TestTenancyHandlers_BurnMyAPIKey_Unauthenticated(t *testing.T) {
	store := newRotateTestStore(t)
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	srv := httptest.NewServer(http.HandlerFunc(h.BurnMyAPIKey))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}
