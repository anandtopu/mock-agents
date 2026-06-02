# Pass 2 — Cross-file integration findings — internal/tenancy

Pass 2 assumes each file is internally sound (Pass 1 covered that) and inspects
only the seams: store ↔ handlers, store ↔ cache, middleware ↔ store, and the
interface ↔ implementation. Blast radius: `internal/server` (handlers,
middleware wiring, route floors) and `cmd/mockagents/start.go` consume this
package; `audit`/`storage` only mention it in comments (no import — convention
holds).

## Findings

| ID | Sev | Conf | Pri | Eff | Sites | Dimension | Evidence → Fix |
|----|-----|------|-----|-----|-------|-----------|----------------|
| **X-TN-001** | **S1** | **High** | **P1** | **L** | `store.go:212,234` (ListTenants/DeleteTenant — bare id, no tenant scope), `server/tenancy_handlers.go` (ListTenants/CreateTenant/DeleteTenant — no `ensureOwnTenant`), `server/route_authz.go:35-37` (RoleAdmin floor only) | Data flow / tenant isolation | **Cross-tenant tenant-CRUD.** "admin" is per-tenant (no super-admin tier), yet `GET/POST /api/v1/tenants` and `DELETE /api/v1/tenants/{id}` are gated by `RequireRole(admin)` only — no ownership check. A tenant-A admin can **enumerate every tenant** (`ListTenants` returns all), **delete another tenant** (`DELETE …/ten_b` → `DeleteTenant(ctx, id)` with the raw path id → cascade-wipes ten_b's keys), and create tenants. This is the unresolved **"store half" of X-SEC-001**. Could be argued S0 (cross-tenant destructive IDOR); held at S1 because it was a *documented, deliberately-deferred* design gap and MockAgents is a dev/mock tool. **Fix is a design decision** (see action plan): (a) introduce a platform/super-admin capability and gate tenant-CRUD to it, (b) scope tenant-CRUD to the caller's own tenant (self-service only), or (c) explicitly document tenant-CRUD as a trusted-bootstrap-operator surface and accept the model. |
| **X-TN-002** | S2 | High | P2 | M | `store.go:618-631` (bcrypt only on prefix match) ↔ `middleware.go:128` (trust boundary) | Security / timing | **Prefix-existence timing oracle** (consolidates F-ST-007 + F-MW-005). `Resolve` runs bcrypt only when the 12-char prefix matches a row; a prefix-miss returns fast. An attacker can probe `mak_<guess>_…` and time the response to learn whether *some* key with that prefix exists. **Low value** — the prefix is 32 random bits and existence alone grants nothing (the 192-bit secret still must be guessed) — but the fix is cheap: run a fixed dummy `bcrypt.CompareHashAndPassword` on a prefix-miss to equalize latency. |
| **X-TN-003** | S3 | High | P3 | S | `store.go:36-89` (Store interface) ↔ `store.go:195` (`GetTenantByName`) | Interface ↔ impl | `GetTenantByName` is a public `SQLiteStore` method **not** on the `Store` interface, contradicting the doc's "mechanical Postgres swap" claim (F-ST-008). Either add it to the interface or rename/document it as a concrete-only bootstrap helper. |

## Verified-sound seams (positive results — recorded so they're not re-audited)

- **X-SEC-001 key-half is correctly enforced.** The tenant-scoped store methods (`DeleteAPIKey`, `RotateAPIKey`, `UpdateAPIKeyRole`) take `tenantID` and filter `AND tenant_id = ?`; the handlers pass `principal.TenantID` (never a client value), and the `{id}`-addressed key routes (`ListAPIKeys`/`CreateAPIKey`/`BulkRotate`) gate on `ensureOwnTenant` (path `{id}` must equal `principal.TenantID`). A cross-tenant **key** operation returns 404. The design answer to "where should tenant scoping live?" is: **in the store method signatures (tenantID param), enforced by handlers passing the authenticated principal's tenant.** This pattern is sound for keys — X-TN-001 is the same pattern *not yet applied* to tenant-CRUD.
- **Auth-cache invalidation is complete.** Every store mutator flushes the cache after commit: `DeleteTenant:246`, `UpdateAPIKeyRole:340`, `RotateAPIKey:407`, `BulkRotateTenantKeys:541`, `DeleteAPIKey:558`. This is *essential* (not optional) because `Resolve` checks `cache.Get` *before* the DB (store.go:587) — without the flush a rotated/revoked key would still authenticate from cache until TTL. No mutator misses it → **no stale-auth vulnerability** (resolves the auth-cache Pass-1 cross-file risk F-AC-005).
- **Single-connection invariant holds.** `MaxOpenConns(1)` + the `foreign_keys(on)` DSN pragma make the `DeleteTenant` cascade and `Resolve`'s drain-then-update pattern correct. Recorded as a fragile invariant (F-ST-004) — a future pool-size bump would silently break FK enforcement under modernc/sqlite.
- **No import-direction violation.** `tenancy` imports `engine`-free; `audit`/`storage` do not import `tenancy` (the DenialHook + principal-extraction-fn indirection works as documented).

## Relationship / blast-radius map

```
cmd/mockagents/start.go ──┐
                          ├─> tenancy.SQLiteStore.EnableAuthCache, CreateTenant, CreateAPIKey (bootstrap)
internal/server/server.go ┘
internal/server/middleware.go  ─> tenancy.AuthMiddleware / ParseBearerToken / PrincipalFrom
internal/server/route_authz.go ─> tenancy.RoleViewer/Editor/Admin (floor table)
internal/server/tenancy_handlers.go ─> tenancy.Store (all CRUD)  ← X-TN-001 lives at this seam
internal/server/handlers.go, audit_handlers.go ─> tenancy.PrincipalFrom

tenancy/middleware.go ─> tenancy/store.go (Resolve)  ← fail-closed verified
tenancy/store.go ─> tenancy/auth_cache.go (Get/Set/Invalidate)  ← invalidation complete
tenancy/store.go, middleware.go ─> tenancy/types.go (Role, Principal, sentinels)
```
