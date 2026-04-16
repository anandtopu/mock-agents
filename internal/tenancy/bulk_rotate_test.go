package tenancy

import (
	"context"
	"testing"
)

func TestBulkRotateTenantKeys_RotatesEverything(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "acme")

	a, _ := store.CreateAPIKey(ctx, tenant.ID, "ci-bot", RoleEditor)
	b, _ := store.CreateAPIKey(ctx, tenant.ID, "viewer-bot", RoleViewer)
	c, _ := store.CreateAPIKey(ctx, tenant.ID, "admin-bot", RoleAdmin)

	results, oldPrefixes, err := store.BulkRotateTenantKeys(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("BulkRotateTenantKeys: %v", err)
	}
	if got := len(results); got != 3 {
		t.Fatalf("results = %d, want 3", got)
	}
	if got := len(oldPrefixes); got != 3 {
		t.Fatalf("oldPrefixes = %d, want 3", got)
	}
	// Index by id so the assertions aren't order-sensitive on the
	// caller side. The store documents ordering by created_at ASC
	// but we don't want to couple the test to that detail.
	byID := make(map[string]*NewAPIKeyResult, 3)
	prefixByID := make(map[string]string, 3)
	for i, r := range results {
		byID[r.Key.ID] = r
		prefixByID[r.Key.ID] = oldPrefixes[i]
	}
	for _, original := range []*NewAPIKeyResult{a, b, c} {
		rotated, ok := byID[original.Key.ID]
		if !ok {
			t.Fatalf("key %q missing from bulk results", original.Key.ID)
		}
		if rotated.Plaintext == original.Plaintext {
			t.Errorf("plaintext unchanged for %q", original.Key.Name)
		}
		if rotated.Key.Prefix == original.Key.Prefix {
			t.Errorf("prefix unchanged for %q", original.Key.Name)
		}
		if rotated.Key.Role != original.Key.Role {
			t.Errorf("role changed for %q: %q -> %q", original.Key.Name, original.Key.Role, rotated.Key.Role)
		}
		if rotated.Key.Name != original.Key.Name {
			t.Errorf("name changed for %q -> %q", original.Key.Name, rotated.Key.Name)
		}
		if rotated.Key.TenantID != tenant.ID {
			t.Errorf("tenant changed for %q: %q", original.Key.Name, rotated.Key.TenantID)
		}
		if prefixByID[original.Key.ID] != original.Key.Prefix {
			t.Errorf("reported old prefix mismatch for %q: %q want %q",
				original.Key.Name, prefixByID[original.Key.ID], original.Key.Prefix)
		}

		// Old plaintext dead, new plaintext resolves to the same
		// key id + role.
		if _, err := store.Resolve(ctx, original.Plaintext); err != ErrInvalidKey {
			t.Errorf("old plaintext for %q still resolves: %v", original.Key.Name, err)
		}
		p, err := store.Resolve(ctx, rotated.Plaintext)
		if err != nil {
			t.Errorf("new plaintext for %q does not resolve: %v", original.Key.Name, err)
			continue
		}
		if p.KeyID != original.Key.ID || p.TenantID != tenant.ID || p.Role != original.Key.Role {
			t.Errorf("principal for %q = %+v", original.Key.Name, p)
		}
	}
}

func TestBulkRotateTenantKeys_EmptyTenantIsNoOp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "empty")

	results, oldPrefixes, err := store.BulkRotateTenantKeys(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("BulkRotateTenantKeys: %v", err)
	}
	if len(results) != 0 || len(oldPrefixes) != 0 {
		t.Errorf("expected empty slices, got %d / %d", len(results), len(oldPrefixes))
	}
}

func TestBulkRotateTenantKeys_UnknownTenant(t *testing.T) {
	store := newTestStore(t)
	_, _, err := store.BulkRotateTenantKeys(context.Background(), "ten_bogus")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBulkRotateTenantKeys_ExcludesSpecifiedKeys(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "acme")
	a, _ := store.CreateAPIKey(ctx, tenant.ID, "admin-bot", RoleAdmin)
	b, _ := store.CreateAPIKey(ctx, tenant.ID, "ci-bot", RoleEditor)
	c, _ := store.CreateAPIKey(ctx, tenant.ID, "viewer-bot", RoleViewer)

	// Exclude key a — only b and c should be rotated.
	results, _, err := store.BulkRotateTenantKeys(ctx, tenant.ID, a.Key.ID)
	if err != nil {
		t.Fatalf("BulkRotateTenantKeys: %v", err)
	}
	if got := len(results); got != 2 {
		t.Fatalf("results = %d, want 2 (a excluded)", got)
	}
	// a's old plaintext must still work.
	p, err := store.Resolve(ctx, a.Plaintext)
	if err != nil {
		t.Fatalf("excluded key a should still resolve: %v", err)
	}
	if p.KeyID != a.Key.ID {
		t.Errorf("p.KeyID = %q, want %q", p.KeyID, a.Key.ID)
	}
	// b and c must be rotated.
	if _, err := store.Resolve(ctx, b.Plaintext); err == nil {
		t.Error("key b should have been rotated")
	}
	if _, err := store.Resolve(ctx, c.Plaintext); err == nil {
		t.Error("key c should have been rotated")
	}
	// New plaintexts for b and c should resolve.
	for _, r := range results {
		if _, err := store.Resolve(ctx, r.Plaintext); err != nil {
			t.Errorf("new plaintext for %q does not resolve: %v", r.Key.Name, err)
		}
	}
}

func TestBulkRotateTenantKeys_FlushesAuthCache(t *testing.T) {
	store := newTestStore(t)
	store.EnableAuthCache(0, 16)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "cache-test")
	a, _ := store.CreateAPIKey(ctx, tenant.ID, "a", RoleAdmin)

	// Warm the cache.
	if _, err := store.Resolve(ctx, a.Plaintext); err != nil {
		t.Fatalf("warm Resolve: %v", err)
	}
	if _, _, err := store.BulkRotateTenantKeys(ctx, tenant.ID); err != nil {
		t.Fatalf("BulkRotateTenantKeys: %v", err)
	}
	// Old plaintext must not resolve via the cache.
	if _, err := store.Resolve(ctx, a.Plaintext); err != ErrInvalidKey {
		t.Errorf("old plaintext still resolves: %v", err)
	}
}
