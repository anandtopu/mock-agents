// Static (non-live) logs table. Shared by the SSR path on /logs and
// extracted from page.tsx because Next.js page files can only export
// `default` plus a small set of config keys.

import Link from "next/link";

import type { InteractionLog } from "@/lib/api";

export function LogsTable({ logs }: { logs: InteractionLog[] }) {
  if (logs.length === 0) {
    return null;
  }
  return (
    <div className="table-wrap">
      <table className="log-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Agent</th>
            <th>Method · Path</th>
            <th>Scenario</th>
            <th>Status</th>
            <th>Latency</th>
            <th>Cost</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {logs.map((log) => (
            <tr key={log.id}>
              <td>{formatTimestamp(log.timestamp)}</td>
              <td>
                {log.agent_name ? (
                  <Link href={`/agents/${log.agent_name}`}>{log.agent_name}</Link>
                ) : (
                  <span className="muted">—</span>
                )}
              </td>
              <td>
                <code>
                  {log.request_method ?? "POST"} {log.request_path ?? "—"}
                </code>
              </td>
              <td>{log.scenario_name || <span className="muted">—</span>}</td>
              <td>{log.status_code ?? "—"}</td>
              <td>{log.latency_ms !== undefined ? `${log.latency_ms.toFixed(1)} ms` : "—"}</td>
              <td>{log.cost_usd !== undefined ? `$${log.cost_usd.toFixed(6)}` : "—"}</td>
              <td>
                <Link href={`/logs/${log.id}`} className="muted">
                  view →
                </Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
