# Changelog

All notable changes to MockAgents are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/).

MockAgents has not cut a tagged release yet; the version headings below mark the
internal **v0.1 → v0.2 → v0.3** development milestones. All three are on `main`.

---

## [Unreleased]

### Added
- **OpenAI Moderations API** (A-07) — a new `POST /v1/moderations` surface
  (omni-moderation shape) for testing guardrail pipelines offline. Returns
  `flagged` + the full set of 13 category booleans, `category_scores`, and
  `category_applied_input_types`, **deterministically**: a keyword→category map
  flags known-harmful phrases (with word-boundary matching, so "skill" doesn't
  trip "kill") at a high score while benign text stays low, over an FNV-seeded
  per-category baseline so scores are realistic and stable across runs. Accepts
  `input` as a string, string array, or content parts; one result per input.
  Zero-config (no agent definition); engine-free like `/v1/embeddings`. Free on
  the real API, so it is deliberately excluded from quota/cost accounting.
- **OpenAI structured outputs / `response_format`** (A-03) — Chat Completions
  now honors `response_format`. With `{type:"json_schema", json_schema:{schema,
  strict}}` the mock returns assistant `content` that is a JSON string
  **conforming to the supplied schema** (so an SDK `.parse()` — Pydantic / Zod —
  round-trips), synthesized deterministically from the schema when the scenario
  doesn't already supply valid JSON. Handles nested objects, arrays, `$ref`/
  `$defs` (incl. recursive), `anyOf`/`oneOf`/`allOf`, `enum`/`const`, nullable
  type arrays, and string `format`s, with depth + array + total-node budgets so
  a hostile/recursive schema can't blow up. `{type:"json_object"}` guarantees a
  JSON object. A planted refusal surfaces as `message.refusal` +
  `finish_reason:"content_filter"`. Example: `examples/structured-output-agent.yaml`.
- **OpenAI Embeddings API** (A-02) — a new `POST /v1/embeddings` surface
  returning **deterministic, L2-normalized vectors** seeded from a hash of
  (input, model, dimensions), so the same request always yields identical
  vectors offline. Matches the real wire shape (`object:"list"`,
  `data[].object:"embedding"`, input-only `usage`), supports `input` as a
  string / string array / token-id array(s), configurable `dimensions`
  (reduce-only cap to the model's native width), and `encoding_format`
  `float` (default) or `base64`. Zero-config — any embedding model name works
  without an agent definition. Also adds the three `text-embedding-*` models to
  the cost table and wires `/v1/embeddings` (and, fixing an A-01 gap,
  `/v1/responses`) into the billable-path classifier so both are logged and
  quota-counted.
- **OpenAI Responses API** (A-01) — a new `POST /v1/responses` surface, the
  default transport of the OpenAI Agents SDK. Supports the polymorphic `input`
  (bare string or typed item array incl. `function_call_output`), `instructions`,
  typed output items (`message` with `output_text`/`refusal` parts and
  `function_call` items), Responses-shaped `usage`, and request-setting echo
  (tools, tool_choice, text, reasoning, temperature, …). **Streaming** emits the
  full named-event ladder (`response.created` → `in_progress` →
  `output_item.added` → `content_part.added` → `output_text.delta`/`.done` →
  `content_part.done` → `output_item.done` → `completed`, plus
  `function_call_arguments.delta`/`.done`) with monotonic `sequence_number`.
  **Stateful** `previous_response_id` replays prior turns from a bounded
  in-memory store, so Agents-SDK tool loops work. Built-in tools
  (`web_search`/`file_search`/…) are accepted as stubs. Chaos errors render in
  the OpenAI error envelope; bodies are size-capped like the other adapters.
- **Scenario-pack templates** (FB-01) — `mockagents init --template <name>` /
  `--list-templates` scaffold a runnable project (agent + a matching TestSuite +
  README) from five curated, embedded packs: `basic`, `customer-support`, `rag`,
  `coding-agent`, `planner`. A docs gallery catalogs every example pack.
- **Hallucination fixtures** (FB-02) — a `hallucination` block on a scenario
  response (typed fault + ground truth) advertised via the
  `X-Mockagents-Hallucination` header, for testing a client's grounding guardrails.
- **Runtime agent write API** (FB-04) — `POST /api/v1/agents` (create),
  `PUT /api/v1/agents/{name}` (replace), `DELETE /api/v1/agents/{name}`: create,
  edit, and remove agents at runtime with no restart (YAML or JSON, validated,
  editor-gated, audited as `agent.created`/`agent.updated`/`agent.deleted`). Plus
  `mockagents add`/`rm` CLI and **Save/Delete in the GUI console** (FB-06) —
  completing the YAML + CLI + API + GUI quadfecta.
- **Failure / error catalog** (FB-03) — `chaos.errors.fail_first` (fail the first
  N requests then recover, for retry/backoff testing); **provider-accurate**
  injected error bodies + `Retry-After` for OpenAI/Anthropic/Gemini; named chaos
  **presets** (`server-down`, `rate-limited`, `access-denied`, `unauthorized`,
  `flaky`, `slow`); and **semantic** response faults (`finish_reason` override,
  `refusal`, malformed tool-call `raw_arguments`) — honored on streaming too.
- **Load-test target** (FB-05) — distribution-based stream timing
  (`ttft_p50_ms`/`ttft_p95_ms`/`itl_p50_ms`/`itl_p95_ms`, lognormal-sampled per
  request) plus k6 + Locust recipes and a "load-test your LLM app for free" guide.
- **GUI console redesign** — the Next.js web console was restyled end-to-end to
  the "MockAgents Console" design system: a `--sr-*` design-token foundation
  with a light/dark theme toggle (SSR-safe, no flash), a new grouped sidebar
  shell with breadcrumbs, and every surface (agent catalog, agent detail, logs,
  costs, audit, pipelines, editor, tenants/keys, account) rebuilt to the design.
  Icons render as JSX (no `dangerouslySetInnerHTML`). (§2.55)
- **Homelab deployment scripts** — a `homelabsetup/` suite that provisions a K3s
  cluster (`bootstrap-homelab.sh`: K3s + MetalLB + an in-cluster registry +
  containerd mirror) and deploys MockAgents via the bundled Helm chart
  (`deploy-homelab.sh`: build/push an immutable `build-<ts>` image, render
  `examples/` into a ConfigMap, `helm upgrade --install` with a Traefik ingress
  on `mockagents.local`). Includes `fresh-deploy`, `stop`/`restart` (pause/resume
  via replica annotations), and `cleanup` lifecycle scripts plus a
  `DEPLOY_MOCKAGENTS.md` guide. Supports `--multi-tenant` (captures the bootstrap
  admin key) and `--persist` (PVC-backed SQLite log).

### Changed
- Documentation refresh: `CHANGELOG.md` rebuilt to cover v0.1–v0.3,
  `docs/architecture-diagrams.md` and `docs/sequence-diagrams.md` updated from
  the CLI-only-MVP baseline to the current control-plane architecture, and
  `README.md` RBAC table corrected for the `platform` role.

### Security
- **Bounded request-body decoding** — the OpenAI/Anthropic/Gemini adapter routes
  now cap each decoded request body at 10 MiB (`http.MaxBytesReader`) instead of
  draining it into an unbounded pooled-buffer allocation. An oversized body is
  rejected with `413 Request Entity Too Large` in the provider's own error
  envelope. Closes an unbounded-allocation DoS on every adapter route.

### Fixed
- **License detection** — `LICENSE` now carries the full verbatim Apache-2.0
  text (the previous truncated header with an embedded copyright line made
  GitHub report `NOASSERTION`); the project copyright notice moved to a new
  `NOTICE` file.

---

## [v0.3.0] — Control plane, MCP duplex, SDK parity

### Added
- **Multi-tenant control plane GUI** — cookie-based admin auth (`/login`),
  tenant CRUD, and per-tenant API-key management (mint, role change, rotate)
  in the web console. (§2.32)
- **MCP v0.3 bidirectional transport** — server-initiated `sampling/createMessage`
  and `roots/list` flow over an SSE duplex channel: clients subscribe to
  `GET /mcp/events` and POST replies to `POST /mcp/response`. In-process
  primitives `Server.SendRequest` / `Sample` / `ListRoots`; `POST /mcp/sample`
  and `/mcp/roots` admin triggers for black-box tests. (§2.33)
- **Real-time log feed over SSE** — `GET /api/v1/logs/stream` plus a same-origin
  GUI proxy; new interaction rows appear sub-second after the SQLite write.
  (§2.34)
- **Schema-aware config editor** — `POST /api/v1/config/validate` and a GUI
  `/editor` running the *same* validator as `mockagents validate` (no
  client-side schema duplication). (§2.35)
- **Pipeline DAG viewer + management API** — `GET /api/v1/pipelines[/{name}]`
  and a read-only SVG DAG view in the console. (§2.36)
- **API-key rotation** — `POST /api/v1/keys/{id}/rotate` regenerates a secret in
  place (stable id/name/role/tenant), flushes the auth cache, and emits an
  `api_key.rotated` audit event with old + new prefixes. (§2.37)
- **Self-service key rotation + burn** — `POST /api/v1/keys/me/rotate` lets any
  authenticated operator rotate their own key; `POST /api/v1/keys/me/burn`
  rotates without returning the new plaintext (emergency response to a
  compromised browser session). GUI `/account` surface. (§2.47, §2.50)
- **Bulk + selective tenant-key rotation** — `POST /api/v1/tenants/{id}/keys/rotate`
  rotates every key in a tenant transactionally; `?except=self` spares the
  caller's own key so an admin can't lock themselves out. (§2.49, §2.51)
- **MCP bidirectional helpers in all three SDKs** — `McpClient` / `McpEvent` /
  `McpEventStream` with identical surfaces in Python, TypeScript, and Go.
  (§2.39, §2.40, §2.41)
- **Go SDK streaming + in-process engine** — `ChatStream` / `MessageStream` /
  `IterStream` and `NewInProcessClient`, which runs an engine + `httptest.Server`
  inline so Go tests skip the subprocess. (§2.31)
- **Multi-kind validation** — `Pipeline`, `TestSuite`, and `MCPServer` documents
  validate under `mockagents validate`, including pipeline graph checks (cycle +
  unreachable-node detection) and a second cross-document pass that resolves
  every agent/target/node reference across a directory. (§2.38, §2.42, §2.43,
  §2.45, §2.46)
- **Aggregate SSE stream metrics** — admin-gated `GET /api/v1/logs/stream/metrics`
  snapshot of every subscriber's drop count and buffer utilization; the GUI
  surfaces backpressure as a sticky badge. (§2.44, §2.48)

### Changed
- **`platform` super-admin role** — RBAC is now ordered
  `viewer < editor < admin < platform`. Managing the tenant *collection*
  (`GET/POST /api/v1/tenants`, `DELETE /tenants/{id}`) requires `platform`,
  which is minted only by the CLI bootstrap; the management API refuses to
  assign it, so a per-tenant admin cannot self-escalate. (§2.53)
- **Localhost bind by default** — `mockagents start` binds `127.0.0.1`;
  container/remote deployments opt in with `--host 0.0.0.0` /
  `MOCKAGENTS_HOST`. (§2.52)
- **Tenant scope derives from the API-key principal**, not the spoofable
  `X-Mockagents-Tenant` header; `/v1/models` and logs/costs/streams are
  tenant-scoped. (§2.52)

### Security
- **Two-package multi-pass security review hardening** — fixed a cross-tenant
  API-key IDOR (a tenant admin could rotate/delete/promote another tenant's
  key), unified every management route behind a single role-floor table +
  `mountManaged` chokepoint that panics on an un-floored route, repaired
  silently-unmounted live-feed routes + SSE flush, made auth fail closed, and
  added body-size caps, uniform error envelopes, and YAML-alias-bomb bounds.
  Each fix is neuter-verified. (§2.53)
- **GUI security hardening** — `Secure`/`SameSite=Strict` HttpOnly cookie,
  one-time key plaintext via a server-side flash store (never URLs), same-origin
  guards on proxy routes, and a CSP + security-header set. (`docs/SECURITY-REVIEW-GUI.md`)
- **Privacy & retention controls** — `MOCKAGENTS_LOG_BODIES`
  (`full`|`sanitized`|`none`) gates response-body capture and
  `MOCKAGENTS_LOG_MAX_ROWS` bounds the interaction table.

### Performance
- **Hot-path optimizations** — O(1) tenant→model index replacing a per-request
  O(n) scan, skipping the no-op tracing wrapper, coarsened auth `last_used`
  writes, a pooled response encoder, memoized match lowering, and single-copy
  body capture. Each is benchmark-guarded and neuter-verified;
  `docs/PERFORMANCE.md` is the handoff doc, `docs/benchmarks/latest.{json,md}`
  the checked-in baseline. (§2.52, §2.54)
- **`govulncheck` remediation** — `toolchain go1.26.4` + `golang.org/x/net`
  upgrade clear all reachable stdlib/net vulnerabilities.

---

## [v0.2.0] — Performance, streaming parity, observability surfaces

### Added
- **TypeScript and Python SDK streaming parity** — `chatStream`/`messageStream`/
  `iterStream` (TS) and `message_stream`/`iter_stream` (Py) plus `StreamChunk`.
- **GUI v0.2 surfaces** — cost dashboard, audit log viewer, per-row log detail,
  and the first live feed.
- **MCP v0.2** — `completion/complete`, `logging/setLevel`, and a server-emitted
  notification queue with an admin notify endpoint.
- **Tenant-scoped agent isolation** — `metadata.tenant_id`, engine tenant
  context (`engine.WithTenantID` / `TenantIDFromContext`), registry `*ForTenant`
  visibility methods, and the opt-in `X-Mockagents-Tenant` header.
- **Cost estimation** — per-model price table (`internal/pricing`), `cost_usd`
  log annotation, and `GET /api/v1/costs`; `MOCKAGENTS_PRICING` loads overrides.
- **Audit logging** — append-only SQLite log of control-plane mutations with
  `GET /api/v1/audit`, including `auth.denied` and role-change events.
- **Streaming cassette capture** — record/replay now tees and replays SSE
  streams.
- **Helm chart v0.2** — opt-in HPA, PodDisruptionBudget, NetworkPolicy, and
  ServiceMonitor.

### Performance
- **v0.2 perf workstream** — pooled JSON decode buffers (-39 % B/op vs
  `json.NewDecoder`), a bounded async log-worker pool (replacing unbounded
  goroutine fan-out), a TTL auth cache that skips bcrypt on repeat resolutions
  (~36 ms cold → sub-µs hot), SQLite multi-conn pool (`MaxOpenConns=8` +
  `synchronous=NORMAL`), session history pre-sizing, tracer NoOp bypass, and
  pooled template/response buffers. Hot path moved -10 % to -24 %.

---

## [v0.1.0] — Foundation MVP

### Added
- **Agent definitions** — declarative `mockagents/v1` YAML with JSON-schema
  validation (`schema/mockagents-v1-agent.json`).
- **Mock engine** — scenario matching (`content_contains`, `content_regex`,
  `turn_number`, `default`), 15+ template functions (`{{ uuid }}`,
  `{{ random_int }}`, `{{ fake_name }}`, …), and conversation state management.
- **Tool-call simulation** — match-based tool responses, error injection,
  parameter validation, and parallel processing.
- **Protocol adapters** — wire-compatible OpenAI Chat Completions
  (`/v1/chat/completions`) and Anthropic Messages (`/v1/messages`), each with
  non-streaming and SSE-streaming modes.
- **HTTP server** — `net/http` multi-agent routing, middleware (auth, logging,
  CORS), graceful shutdown, fsnotify hot reload (`--watch`), and a management
  API.
- **Multi-agent pipelines** (`kind: Pipeline`) — sequential, parallel, and graph
  topologies with substring-matched conditional edges.
- **TestSuite runner** (`kind: TestSuite`) — declarative cases with
  `tool_call` / `response_contains` / `scenario_matched` / `latency_ms_lt`
  assertions; `mockagents test` emits text/JSON/JUnit.
- **Chaos engineering** — per-agent `latency`, `errors`, and `rate_limit`
  injection, evaluated before tool resolution.
- **Record & playback** — proxy a real upstream once (`mockagents record`),
  replay offline forever (`mockagents replay`); cassettes are JSON-lines.
- **Mock MCP server** (`kind: MCPServer`) — JSON-RPC 2.0 over HTTP + stdio
  (`mockagents mcp`).
- **Contract testing** — `mockagents contract extract | diff` classifies changes
  as breaking / additive / info for CI.
- **OpenTelemetry tracing** — opt-in OTLP/HTTP exporter, no-op (zero overhead)
  by default.
- **SDKs** — Python (`MockAgentServer`, `MockAgentClient`, `expect()` assertions,
  LangChain/LangGraph/CrewAI adapters), TypeScript, and Go.
- **Multi-tenant auth + RBAC** (opt-in `MOCKAGENTS_MULTI_TENANT=1`) — bcrypt API
  keys, `viewer`/`editor`/`admin` roles, and a bootstrap admin key.
- **Web console v0.1** (Next.js 15) — agent catalog and interaction-log views.
- **Interaction logging** — pure-Go SQLite (`modernc.org/sqlite`, no cgo) with a
  query API and `mockagents logs`.
- **Packaging** — single static binary, multi-stage Docker image,
  docker-compose, Helm chart v0.1, and GitHub Actions / GitLab CI templates.

### Protocol support
- OpenAI Chat Completions API (non-streaming + SSE streaming)
- Anthropic Messages API (non-streaming + SSE streaming)
- Model Context Protocol (JSON-RPC 2.0, HTTP + stdio)

### CLI commands
- `init`, `start`, `validate`, `logs`, `test`, `record`, `replay`, `mcp`,
  `contract`
