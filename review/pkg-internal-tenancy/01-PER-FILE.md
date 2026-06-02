# Pass 1 — Per-file findings — internal/tenancy

Deep mode. Each file reviewed in isolation against the 9-dimension checklist.
Severities reflect post-adversarial-verification adjustments (see notes in
`00-SUMMARY.md`); the original subagent ratings are noted where downgraded.

## store.go (659 LOC) — SQLite store: tenants, bcrypt API keys, RBAC

| ID | Sev | Conf | Pri | Eff | Site | Dimension | Evidence → Fix |
|----|-----|------|-----|-----|------|-----------|----------------|
| F-ST-001 | S2 | High | P2 | S | store.go:639 | entropy/err | `randID` does `_, _ = rand.Read(b[:])`, discarding the error → a crypto/rand failure yields an all-zero, predictable id. **Downgraded from S1**: ids are non-secret (auth is the bcrypt'd plaintext, and `generateAPIKey` *does* check rand errors); a zero id at worst collides on the PRIMARY KEY (surfaced as an error). Fix: return the error and propagate through Create*. |
| F-ST-003 | S2 | High | P2 | S | store.go:578,591 | correctness | `Resolve` floors at `len < 13` then slices `plaintext[:12]` as the prefix; a key whose shape isn't `mak_<8hex>_…` mis-indexes. Validate the `mak_<8hex>_` shape, not a bare length floor. |
| F-ST-005 | S2 | Med | P3 | S | store.go:189,207,227,307,409,520 | err handling | `time.Parse(...)` errors discarded throughout the read paths → a corrupt timestamp column silently becomes the zero time. Log/wrap on parse failure. |
| F-ST-009 | S2 | High | P2 | M | store.go:431-543 | tests | `BulkRotateTenantKeys` all-or-nothing rollback (its whole point) is **untested** — `bulk_rotate_test.go` covers success/empty/unknown/exclude/cache-flush but never injects a mid-loop failure to prove zero rows mutate on rollback. Add a fault-injection test. |
| F-ST-010 | S2 | High | P2 | M | store.go:321-419,547-560 | tests | Cross-tenant scoping of `RotateAPIKey`/`DeleteAPIKey`/`UpdateAPIKeyRole` (the X-SEC-001 key fix — wrong-tenant id → ErrNotFound) has **no store-level test**. Add IDOR regression tests at the store layer for all three. |
| F-ST-012 | S2 | Med | P3 | S | store.go:257-259 | perf | `CreateAPIKey` does a `GetTenant` round-trip before insert, duplicating the FK constraint's own guarantee. Acceptable for a nicer error, but relies on `foreign_keys(on)` being active (see F-ST-004). |
| F-ST-013 | S2 | Med | P3 | S | store.go:435-437 | correctness | `BulkRotateTenantKeys` existence-checks the tenant *outside* the tx; a delete in the race window makes the in-tx SELECT return 0 rows and the method commits empty + returns `(nil,nil,nil)` instead of ErrNotFound. **Downgraded from S1**: benign (the tenant is gone; rotating nothing is harmless) — only the return is misleading. Move the check inside the tx or document the semantics. |
| F-ST-014 | S2 | Low | P3 | S | store.go:328-335 | perf/concurrency | `UpdateAPIKeyRole` does SELECT-then-UPDATE non-atomically (serialized by MaxOpenConns=1 in practice). Use `UPDATE … RETURNING` or a tx for a truly atomic read-then-write. |
| F-ST-002 | S3 | High | P3 | S | store.go:264,387,509 | security | `bcrypt.DefaultCost` (10) repeated at 3 sites as a magic value. The 192-bit secret means cost isn't the weak link, but extract a named const so it's reviewable in one place. |
| F-ST-004 | S3 | High | P3 | S | store.go:129-130 | contract | The `DeleteTenant` cascade + DSN `foreign_keys(on)` correctness both depend on `SetMaxOpenConns(1)`. Add a comment binding that invariant so a future pool-size bump doesn't silently drop FK enforcement. |
| F-ST-006 | S3 | Med | P3 | S | store.go:622-625 | err handling | `Resolve`'s best-effort `last_used` UPDATE discards its error (documented/acceptable); a persistently failing write is invisible. Consider a debug log. |
| F-ST-007 | S3 | Med | P3 | M | store.go:618-631 | timing | `Resolve` short-circuits on bcrypt match but runs bcrypt for every same-prefix candidate on a miss — a theoretical timing asymmetry. Folded into **X-TN-003**. |
| F-ST-008 | S3 | High | P3 | S | store.go:195-209 | API | `GetTenantByName` is a public `SQLiteStore` method but not on the `Store` interface — drift vs the "mechanical Postgres swap" claim. Folded into **X-TN-005**. |
| F-ST-011 | S3 | High | P3 | S | store.go:61-76 | readability | The `Store` interface's `BulkRotateTenantKeys` doc block is **duplicated** (two `// BulkRotateTenantKeys …` openers). Merge into one. |
| F-ST-015 | S3 | High | P3 | S | store.go:578,591,650 | readability | Magic `13`/`12` prefix lengths are co-dependent literals implicitly defined by `generateAPIKey` (`mak_`+8hex=12). Extract `const apiKeyPrefixLen = 12` so the two can't drift. |

## middleware.go (177 LOC) — AuthMiddleware, RequireRole, bearer parsing, DenialHook

| ID | Sev | Conf | Pri | Eff | Site | Dimension | Evidence → Fix |
|----|-----|------|-----|-----|------|-----------|----------------|
| F-MW-001 | S2 | High | P3 | S | middleware.go:90 | security | Bearer parser trims the remainder but accepts internal whitespace (`Bearer ab cd` → token `"ab cd"`). Harmless (a real key has none) but muddies the contract; reject tokens containing internal whitespace. |
| F-MW-003 | S2 | Med | P3 | S | middleware.go:114-118 | err handling | On a **skip-auth** route, a raw store error during Resolve is swallowed → the caller proceeds anonymously (fail-OPEN). Bounded to health/LLM routes where anonymity is intended; add a one-line comment stating the deliberate best-effort contract. |
| F-MW-002 | S3 | High | P3 | S | middleware.go:128-138 | err handling | The 401 branch matches only `ErrInvalidKey`; an `ErrNotFound` would 500. **Verified defensive-only**: `Resolve` never returns ErrNotFound (only ErrInvalidKey/raw/nil), and the fallthrough is fail-*closed* (safe). Optionally add `|| errors.Is(err, ErrNotFound)` for label accuracy. |
| F-MW-004 | S3 | High | P3 | — | middleware.go:128-160 | security | **Verified clean**: same `ErrInvalidKey` for missing vs wrong-hash (no existence oracle at store level); RequireRole's 403 echoes only the caller's *own* role. No change. |
| F-MW-005 | S3 | High | P3 | M | middleware.go:128 | timing | The trust boundary over the store's prefix-timing oracle. Folded into **X-TN-003**. |
| F-MW-006 | S3 | High | P3 | S | middleware.go:156 | correctness | **Verified correct**: `AtLeast` + `rank()` (viewer<editor<admin, unknown→-1) means a viewer can't satisfy an admin gate and an unknown role satisfies nothing. The `required.rank() > 0` guard is load-bearing — add a unit assertion so a refactor can't drop it (see F-TY-005). |
| F-MW-007 | S3 | Med | P3 | S | middleware.go:148-165 | API/doc | RequireRole's doc reads as a complete authorization guarantee but it checks **level only**, not tenant ownership. Add: "tenant-ownership scoping is the handler's responsibility." |
| F-MW-008 | S3 | High | P3 | S | middleware.go:135-136 | tests | The 500 fail-closed branch (non-ErrInvalidKey error from Resolve) is **untested**. Add a fake-Store-returns-error test asserting 500 + `next` not reached. |
| F-MW-009 | S3 | Med | P3 | S | middleware.go:114-118 | tests | The skip-route principal-attachment behavior (valid key on a skip route still populates the principal) is untested. Add coverage. |

## auth_cache.go (135 LOC) — bounded TTL cache fronting bcrypt

| ID | Sev | Conf | Pri | Eff | Site | Dimension | Evidence → Fix |
|----|-----|------|-----|-----|------|-----------|----------------|
| F-AC-001 | S2 | High | P2 | S | auth_cache.go:20,60-63 | security/doc | Doc says key = `sha256(plaintext)[:32]` (256-bit) but code uses `sum[:16]` (**128-bit** truncation). Collision → cross-key auth confusion (S0 *in principle*) but infeasible at maxSize=1024. Fix the misleading doc; optionally key on the full 32-byte digest (cache isn't memory-bound). |
| F-AC-003 | S2 | Med | P2 | S | auth_cache.go:99-104 | correctness | Capacity eviction is **random** (`for k := range … break`) — it can evict a hot non-expired entry while leaving an expired one resident, so a hot key re-runs bcrypt under pressure. Scan-and-drop an expired entry first, then fall back to random. |
| F-AC-005 | S2 | Med | P3 | S | auth_cache.go:79-82 | security | Expiry is lazy (only on Get); the cache's safety depends on the store calling `Invalidate()` on every mutation. **Verified in Pass 2: all 5 mutators flush.** Residual: TTL is the worst-case stale-auth bound — document it. |
| F-AC-002 | S3 | High | P3 | S | auth_cache.go:62 | security/doc | "collision-safe for <2^64 entries" is birthday-bound-wrong. Fix the comment. |
| F-AC-004 | S3 | Med | P3 | S | auth_cache.go:99 | correctness | At capacity, re-`Set` of an existing key still evicts a *different* entry before overwriting. Guard with `if _, exists := …; !exists && len >= max`. |
| F-AC-006 | S3 | High | P3 | S | auth_cache.go:29,73 | perf | A single `sync.Mutex` serializes all Gets, and Get takes the *write* lock to lazy-delete expired entries. Consider `RWMutex` (RLock on the hit path) for the hot read path. |
| F-AC-007 | S3 | Low | P3 | — | auth_cache.go:79 | concurrency | No TOCTOU (delete-then-return under the held lock is sound); informational, folds into F-AC-006. |
| F-AC-008 | S3 | High | P3 | — | auth_cache.go:68,92 | security | **Verified sound**: Get/Set take plaintext and hash internally (no plaintext stored); returned `*Principal` can't poison the cache because `Principal` has only value fields. No action. |
| F-AC-009 | S3 | Med | P3 | S | auth_cache.go:92-95 | err handling | `Set` no-ops on nil principal but never guards `plaintext == ""`. Add `if plaintext == "" { return }` to Get and Set. |
| F-AC-010 | S3 | Med | P3 | S | auth_cache.go:126-135 | API | No hit/miss/eviction metrics despite the comment anticipating them. Optional. |

## types.go (100 LOC) — Role, Principal, Tenant, APIKey, NewAPIKeyResult, sentinels

| ID | Sev | Conf | Pri | Eff | Site | Dimension | Evidence → Fix |
|----|-----|------|-----|-----|------|-----------|----------------|
| F-TY-003 | S2 | High | P2 | S | types.go:75-78 | security | `NewAPIKeyResult.Plaintext` is a plain `string` with no redaction; an accidental `%v`/`slog` spills the secret. **No current leak** (handler returns it once; audit logs the prefix), but add a `LogValue()`/redaction so accidental logging can't spill it (defense-in-depth). |
| F-TY-002 | S2 | High | P2 | S | types.go:62-70 | tests/doc | `APIKey` correctly carries no `Hash`/`Plaintext` field, but this safety is invisible/untested. Add a doc line + a marshal test asserting the JSON has no `hash`/`secret`/`plaintext` key. |
| F-TY-005 | S3 | Med | P2 | S | types.go:31-49 | tests | `rank()`/`AtLeast`/`IsValid` encode the entire RBAC ordering + the unknown→-1 invariant RequireRole relies on, with **no test**. Add a table test (admin.AtLeast(viewer)=true, viewer.AtLeast(admin)=false, "".AtLeast(viewer)=false, IsValid on bogus). |
| F-TY-001 | S3 | High | P3 | S | types.go:69 | conventions | `LastUsed time.Time` with `json:"last_used,omitempty"` — `omitempty` is a no-op for a struct, so a never-used key serializes `"0001-01-01T00:00:00Z"`. Use `*time.Time` or drop the misleading `omitempty`. |
| F-TY-004 | S3 | Low | P3 | S | types.go:44-46 | correctness | `AtLeast` reads `required.rank()` twice; correct but mildly wasteful and the guard intent is undocumented. Hoist to a local + comment. |
| F-TY-006 | S3 | Med | P3 | S | types.go:83-87 | security | `Principal` is context-only (no secrets) but has no `json` tags; a future accidental marshal would expose field names. Add `json:"-"` or a "never serialized" doc. |
| F-TY-007 | S3 | Med | P3 | M | types.go:23-27 | conventions | No `AllRoles()`/`ParseRole(string)` — every inbound-role validator re-derives the set via `IsValid()` on a raw cast. Add a constructor to centralize the canonical set. |
