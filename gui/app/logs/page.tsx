import Link from "next/link";

import { APIError, InteractionLog, listAgents, listLogs } from "@/lib/api";

import { AutoRefreshLogs } from "./AutoRefreshLogs";
import { LogsTable } from "./LogsTable";

export const dynamic = "force-dynamic";

interface LogsPageSearchParams {
  agent?: string;
  since?: string;
  limit?: string;
  live?: string;
}

export default async function LogsPage({
  searchParams,
}: {
  searchParams: Promise<LogsPageSearchParams>;
}) {
  const params = await searchParams;
  const limit = clampLimit(params.limit);
  const agent = params.agent || "";
  const since = params.since || "";
  const live = params.live === "1";

  let logs: InteractionLog[] = [];
  let agents: string[] = [];
  let error: string | null = null;
  try {
    const [rows, allAgents] = await Promise.all([
      listLogs({ limit, agent: agent || undefined, since: since || undefined }),
      listAgents(),
    ]);
    logs = rows;
    agents = allAgents.map((a) => a.name);
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  return (
    <div>
      <h1 className="page-title">Interaction logs</h1>
      <p className="page-lede">
        Recent request/response pairs recorded by the running server.
        Use the filters to narrow the view; click a row to drill in.
      </p>

      <form className="filters" action="/logs" method="get">
        <label>
          Agent
          <select name="agent" defaultValue={agent}>
            <option value="">All agents</option>
            {agents.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </label>
        <label>
          Since (RFC3339)
          <input
            type="text"
            name="since"
            placeholder="2026-04-14T00:00:00Z"
            defaultValue={since}
          />
        </label>
        <label>
          Limit
          <select name="limit" defaultValue={String(limit)}>
            <option value="25">25</option>
            <option value="50">50</option>
            <option value="100">100</option>
            <option value="250">250</option>
          </select>
        </label>
        <label className="toggle">
          <input type="checkbox" name="live" value="1" defaultChecked={live} />
          Live (auto-refresh every 3s)
        </label>
        <button type="submit">Apply</button>
        {(agent || since || live) && (
          <Link href="/logs" className="muted">
            reset
          </Link>
        )}
      </form>

      {error && (
        <div className="banner banner-error">
          <strong>Could not load logs.</strong> {error}
        </div>
      )}

      {!error && logs.length === 0 && !live && (
        <div className="empty">
          No interactions match the current filter. Point a client at
          the MockAgents server and refresh.
        </div>
      )}

      {live ? (
        <AutoRefreshLogs initialLogs={logs} agent={agent} since={since} limit={limit} />
      ) : (
        <LogsTable logs={logs} />
      )}
    </div>
  );
}

function clampLimit(raw: string | undefined): number {
  const n = raw ? Number(raw) : 50;
  if (!Number.isFinite(n) || n <= 0) return 50;
  if (n > 1000) return 1000;
  return Math.floor(n);
}
