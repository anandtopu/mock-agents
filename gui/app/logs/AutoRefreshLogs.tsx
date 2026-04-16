"use client";

// AutoRefreshLogs is the client island for the /logs?live=1 view. It
// starts from an SSR snapshot of rows (initialLogs) then opens an
// EventSource against the same-origin /api/logs/stream proxy and
// prepends each newly-arrived row to the table in place.
//
// v0.3 update: the old 3-second setInterval poll was replaced by a
// real server-sent-events subscription backed by
// GET /api/v1/logs/stream. Per-row updates arrive the moment the Go
// LogWorker finishes its SQLite write, so "live" latency dropped from
// ~3 s worst case to sub-100 ms.

import { useEffect, useRef, useState } from "react";
import Link from "next/link";

import type { InteractionLog } from "@/lib/api";

interface AutoRefreshLogsProps {
  initialLogs: InteractionLog[];
  agent: string;
  since: string;
  limit: number;
}

export function AutoRefreshLogs({
  initialLogs,
  agent,
  limit,
}: AutoRefreshLogsProps) {
  const [logs, setLogs] = useState<InteractionLog[]>(initialLogs);
  const [error, setError] = useState<string | null>(null);
  const [connected, setConnected] = useState(false);
  const [lastEventAt, setLastEventAt] = useState<Date | null>(null);
  // droppedCount is the cumulative number of log events the backend
  // skipped for this subscription because our channel was full.
  // Surfaced by the server as an SSE `event: dropped` frame, which
  // the browser receives via addEventListener("dropped", ...).
  const [droppedCount, setDroppedCount] = useState(0);
  // Reconnect-retry counter survives across StrictMode double-mounts.
  const retryRef = useRef(0);

  useEffect(() => {
    let es: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;

    function connect() {
      if (disposed) return;
      es = new EventSource("/api/logs/stream");

      es.addEventListener("open", () => {
        if (disposed) return;
        setConnected(true);
        setError(null);
        retryRef.current = 0;
      });

      // Custom "log" event per the backend frame format.
      es.addEventListener("log", (evt: MessageEvent<string>) => {
        if (disposed) return;
        let row: InteractionLog;
        try {
          row = JSON.parse(evt.data) as InteractionLog;
        } catch (err) {
          setError(`parse error: ${err instanceof Error ? err.message : "unknown"}`);
          return;
        }
        // Client-side filter — we deliberately do not filter on the
        // server so that one shared stream feeds every open tab.
        if (agent && row.agent_name !== agent) return;
        setLogs((prev) => {
          // Prepend and cap at `limit` so the table never grows
          // unbounded during a long-running live session.
          const next = [row, ...prev];
          return next.slice(0, limit);
        });
        setLastEventAt(new Date());
      });

      // Custom "dropped" event surfaces server-side backpressure
      // on this subscription. Payload is {count, new}: count is
      // cumulative, new is the delta since the last dropped frame.
      // A sticky badge is the right UX because operators almost
      // always want to know their browser tab is falling behind,
      // even briefly — it's an actionable signal.
      es.addEventListener("dropped", (evt: MessageEvent<string>) => {
        if (disposed) return;
        try {
          const payload = JSON.parse(evt.data) as { count?: number };
          if (typeof payload.count === "number") {
            setDroppedCount(payload.count);
          }
        } catch {
          /* ignore malformed — server should never emit one */
        }
      });

      es.addEventListener("error", () => {
        if (disposed) return;
        setConnected(false);
        // EventSource auto-reconnects but only on a clean 200→close.
        // On 502/network errors we retry with capped exponential
        // backoff so a crashed upstream doesn't trigger a
        // reconnect storm.
        es?.close();
        es = null;
        const delayMs = Math.min(30_000, 1000 * Math.pow(2, retryRef.current));
        retryRef.current += 1;
        setError(`reconnecting in ${Math.round(delayMs / 1000)}s…`);
        retryTimer = setTimeout(connect, delayMs);
      });
    }

    connect();
    return () => {
      disposed = true;
      if (retryTimer) clearTimeout(retryTimer);
      if (es) es.close();
    };
  }, [agent, limit]);

  return (
    <div>
      <div className="live-bar">
        <span className={`dot ${connected ? "dot-live" : "dot-down"}`} />
        {connected ? "Live · SSE" : "Disconnected"}
        {lastEventAt && (
          <span className="muted"> · last event {lastEventAt.toLocaleTimeString()}</span>
        )}
        {droppedCount > 0 && (
          <span className="drop-badge" title="Backend dropped log events because this tab's buffer was full.">
            ⚠ {droppedCount} dropped
          </span>
        )}
        {error && <span className="muted"> · {error}</span>}
      </div>

      {logs.length === 0 ? (
        <div className="empty">
          Waiting for traffic. Send a request to the MockAgents server.
        </div>
      ) : (
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
                  <td>
                    {log.latency_ms !== undefined ? `${log.latency_ms.toFixed(1)} ms` : "—"}
                  </td>
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
      )}
    </div>
  );
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
