import { APIError, AuditEvent, listAudit } from "@/lib/api";

export const dynamic = "force-dynamic";

export default async function AuditPage() {
  let events: AuditEvent[] | null = null;
  let error: string | null = null;
  try {
    events = await listAudit({ limit: 200 });
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  return (
    <div>
      <h1 className="page-title">Audit log</h1>
      <p className="page-lede">
        Control-plane mutations and authentication denials recorded by
        the running server. The audit log is independent of the
        interaction log and is always on (see{" "}
        <code>internal/audit/</code>).
      </p>

      {error && (
        <div className="banner banner-error">
          <strong>Could not load audit events.</strong> {error}
        </div>
      )}

      {!error && events === null && (
        <div className="banner banner-warn">
          <strong>Admin access required.</strong> The audit endpoint is
          gated behind the admin role when multi-tenant mode is on.
          Set <code>MOCKAGENTS_ADMIN_TOKEN</code> when launching the
          GUI to read it.
        </div>
      )}

      {events && events.length === 0 && (
        <div className="empty">
          No audit events recorded yet.
        </div>
      )}

      {events && events.length > 0 && (
        <div className="table-wrap">
          <table className="log-table">
            <thead>
              <tr>
                <th>Time</th>
                <th>Kind</th>
                <th>Actor</th>
                <th>Target</th>
                <th>Details</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr key={e.id}>
                  <td>{formatTimestamp(e.timestamp)}</td>
                  <td>
                    <span className={`badge badge-${kindCategory(e.kind)}`}>{e.kind}</span>
                  </td>
                  <td>
                    <ActorCell actor={e.actor} />
                  </td>
                  <td>
                    <code>{e.target || "—"}</code>
                  </td>
                  <td>
                    <DetailsCell details={e.details} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function ActorCell({ actor }: { actor: AuditEvent["actor"] }) {
  if (!actor || actor.name === "anonymous") {
    return <span className="muted">anonymous</span>;
  }
  const role = actor.role ? ` · ${actor.role}` : "";
  const ip = actor.remote_ip ? ` · ${actor.remote_ip}` : "";
  return (
    <span title={`${actor.tenant_id ?? ""}${ip}`}>
      {actor.name}
      {role}
    </span>
  );
}

function DetailsCell({ details }: { details: string | undefined }) {
  if (!details) return <span className="muted">—</span>;
  // Best-effort pretty-print: details is a JSON string blob produced
  // by audit.MarshalDetails. If it parses we render the keys; if not
  // we just show the raw string so nothing is ever silently dropped.
  try {
    const parsed = JSON.parse(details) as Record<string, unknown>;
    const entries = Object.entries(parsed);
    if (entries.length === 0) return <span className="muted">—</span>;
    return (
      <span className="audit-details">
        {entries.map(([k, v]) => (
          <span key={k} className="kv">
            <span className="k">{k}=</span>
            <span className="v">{String(v)}</span>
          </span>
        ))}
      </span>
    );
  } catch {
    return <code>{details}</code>;
  }
}

function kindCategory(kind: string): string {
  if (kind.startsWith("auth.")) return "warn";
  if (kind.startsWith("api_key.") || kind.startsWith("tenant.")) return "info";
  if (kind === "agent.reloaded") return "ok";
  return "muted";
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
