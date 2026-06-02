# Review Summary — internal/tenancy

- **Target:** `internal/tenancy/` (multi-tenant store + bcrypt API keys + RBAC auth middleware + auth cache)
- **Reviewed at:** 2026-06-02  ·  **Depth:** deep (S0/S1 adversarially verified)
- **Scope:** 4 source files, ~1071 LOC (`store.go` 659, `middleware.go` 177, `auth_cache.go` 135, `types.go` 100) + 6 test files glanced for coverage gaps.
- **Reviewer:** multi-pass-review skill (Pass 1 fanned across 4 subagents, one per source file; Pass 2 + synthesis single-threaded)
- **Motivation:** resolve the lingering **X-SEC-001 "store half"** from the `internal/server` review — should tenant scoping live in the store signatures or the handler?

## Verdict

> **GO WITH FIXES** — no S0 confirmed; the API-key tenant isolation and the auth-cache invalidation are verified sound. One S1 (`X-TN-001`, cross-tenant **tenant**-CRUD) is a real isolation gap that needs a design decision, and several S2 security-hardening + test-coverage items remain.

## Findings by severity

| Severity | Count | Notable IDs |
|----------|-------|-------------|
| S0 Blocker | 0 | — |
| S1 High    | 1 | `X-TN-001` (cross-tenant tenant-CRUD) |
| S2 Medium  | 12 | `F-ST-001/003/009/010`, `F-AC-001/003/005`, `F-TY-002/003`, `X-TN-002`, `F-MW-001/003` |
| S3 Low     | ~24 | docs/tests/type-hygiene across all four files |

_(2 Pass-1 findings were downgraded after adversarial verification: F-ST-001 S1→S2 — ids are non-secret; F-ST-013 S1→S2 — benign race. F-MW-002 S1→S3 — Resolve never returns ErrNotFound, and the path is fail-closed.)_

## Top risks (what matters)

1. **Cross-tenant tenant-CRUD** (`X-TN-001`, S1) — a per-tenant admin can enumerate every tenant, **delete another tenant** (cascade-wiping its keys), and create tenants; `RequireRole(admin)` is the only gate and there's no super-admin tier. This *is* the unresolved half of X-SEC-001. Needs a design call (super-admin role vs self-service-scope vs documented trusted-operator model).
2. **`randID` swallows the crypto/rand error** (`F-ST-001`, S2) — a rand failure mints a zero-entropy id (non-secret, so not a vuln, but bad hygiene; `generateAPIKey` correctly *does* check it).
3. **Auth-cache: doc/code bit-width mismatch + random eviction** (`F-AC-001`/`F-AC-003`, S2) — key is 128-bit (not the documented 256), and capacity eviction can drop a hot key while keeping an expired one.
4. **Plaintext secret has no redaction** (`F-TY-003`, S2) — `NewAPIKeyResult.Plaintext` would spill on an accidental `%v`/`slog`; no current leak but defense-in-depth is cheap.
5. **Security-critical paths under-tested** (`F-ST-009/010`, `F-MW-008`, `F-TY-005`, S2) — bulk-rotate rollback, store-level cross-tenant IDOR, middleware fail-closed, and RBAC ordering all rely on reasoning, not tests.

## Verified sound (positive results)

- **X-SEC-001 key-half: correctly enforced.** Store key-mutation methods take `tenantID` (`AND tenant_id = ?`); handlers pass `principal.TenantID`; `{id}` key routes gate on `ensureOwnTenant`. Cross-tenant **key** ops return 404. *Answer to the design question:* scoping belongs in the store signatures, enforced by handlers passing the authenticated tenant — and it's done right for keys.
- **Auth-cache invalidation is complete & essential.** All 5 mutators flush the cache after commit; `Resolve` checks the cache before the DB, so the flush is what prevents a rotated/revoked key authenticating until TTL. No mutator misses it → no stale-auth bug.
- **Auth fails closed.** A non-`ErrInvalidKey` error from `Resolve` → 500 with `next` not reached; same error message for missing vs wrong-hash keys (no existence oracle at the store boundary); RBAC ordering (`AtLeast`/`rank`) correctly rejects unknown/insufficient roles.

## Coverage & confidence

- Passes run: 0, 1, 2, 3, 4.
- Deep mode: S0/S1 adversarially verified — **yes** (the one S1 was reproduced by reading the handler+route+store seam; two S1 candidates were downgraded).
- **Not covered / blind spots:** the 6 test files were *glanced* for coverage gaps, not reviewed line-by-line as code. `start.go`'s bootstrap wiring (how the first admin key/tenant is minted, and whether that key is effectively the "super-admin") was not deeply audited — it's directly relevant to the X-TN-001 decision and should be read before choosing option (1)/(3). The `modernc.org/sqlite` driver's exact behavior under `MaxOpenConns>1` (F-ST-004 invariant) was reasoned, not tested.

## Where to act

Start with the **X-TN-001 design decision** (P1) since it may reshape the handlers/routes the P2 tests target. Then the P2 security cluster (small, independent, high-value), then the P2 test batch to lock the contracts, then the P3 sweep. Full executable checklist in **`03-ACTION-PLAN.md`**.
