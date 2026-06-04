import Link from "next/link";

import { APIError, listPipelines, PipelineSummary } from "@/lib/api";
import { Icon } from "@/lib/icons";

export default async function PipelinesPage() {
  let pipelines: PipelineSummary[] = [];
  let error: string | null = null;
  try {
    pipelines = await listPipelines();
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  return (
    <div>
      <div className="page-head">
        <h1 className="page-title">Pipelines</h1>
        <p className="page-lede">
          Multi-agent topologies declared with <code>kind: Pipeline</code>. Sequential, parallel,
          and graph wiring with substring-matched conditional edges. Click one to see its DAG.
        </p>
      </div>

      {error && (
        <div className="banner banner-error">
          <strong>Server error.</strong> {error}
        </div>
      )}

      {!error && pipelines.length === 0 ? (
        <div className="empty">
          No pipelines loaded. Drop a <code>kind: Pipeline</code> YAML into your agents directory
          and restart the server.
        </div>
      ) : (
        <div className="catalog" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))" }}>
          {pipelines.map((p) => (
            <PipelineCard key={p.name} p={p} />
          ))}
        </div>
      )}
    </div>
  );
}

function PipelineCard({ p }: { p: PipelineSummary }) {
  return (
    <Link href={`/pipelines/${encodeURIComponent(p.name)}`} className="agent-card" style={{ minHeight: 0 }}>
      <div className="ac-top">
        <div className="agent-icon">
          <Icon name="workflow" size={18} />
        </div>
        <div className="grow">
          <h3>{p.name}</h3>
          <div className="ac-proto">
            {p.agent_count} agents · {p.edge_count} edges
          </div>
        </div>
        <span className="badge badge-outline">{p.topology}</span>
      </div>
      {p.description && (
        <p className="ac-desc" style={{ flex: "none" }}>
          {p.description}
        </p>
      )}
    </Link>
  );
}
