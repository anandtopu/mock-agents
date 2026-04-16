import Link from "next/link";
import { notFound } from "next/navigation";

import { getPipeline, PipelineDefinition } from "@/lib/api";
import { DAGViewer } from "./DAGViewer";

type PageProps = {
  params: Promise<{ name: string }>;
};

export default async function PipelineDetailPage({ params }: PageProps) {
  const { name } = await params;
  const pipeline = await getPipeline(name);
  if (!pipeline) notFound();

  const agents = pipeline.spec.agents ?? [];
  const edges = normalizeEdges(pipeline);

  return (
    <div>
      <div className="breadcrumb">
        <Link href="/pipelines">Pipelines</Link> · <code>{name}</code>
      </div>
      <h1 className="page-title">{name}</h1>
      {pipeline.metadata.description && (
        <p className="page-lede">{pipeline.metadata.description}</p>
      )}
      <div className="pipeline-meta">
        <span className="tag">{pipeline.spec.topology}</span>
        <span>
          {agents.length} agent{agents.length === 1 ? "" : "s"}
        </span>
        {edges.length > 0 && (
          <span>
            {edges.length} edge{edges.length === 1 ? "" : "s"}
          </span>
        )}
      </div>

      <DAGViewer
        topology={pipeline.spec.topology}
        agents={agents}
        edges={edges}
      />

      <section>
        <h2 className="section-title">Agents</h2>
        <table className="data-table">
          <thead>
            <tr>
              <th>Node ID</th>
              <th>Agent ref</th>
            </tr>
          </thead>
          <tbody>
            {agents.map((a) => (
              <tr key={a.id}>
                <td>
                  <code>{a.id}</code>
                </td>
                <td>
                  <Link href={`/agents/${encodeURIComponent(a.ref)}`}>
                    {a.ref}
                  </Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  );
}

// normalizeEdges converts the three topology shapes into a single
// adjacency list the viewer can render. Sequential pipelines implicit
// a linear chain; parallel pipelines have no edges; graph pipelines
// hand us edges directly.
function normalizeEdges(pipeline: PipelineDefinition) {
  const agents = pipeline.spec.agents ?? [];
  const topology = pipeline.spec.topology;
  if (topology === "sequential") {
    const out: { from: string; to: string; when_contains?: string }[] = [];
    for (let i = 0; i + 1 < agents.length; i++) {
      out.push({ from: agents[i].id, to: agents[i + 1].id });
    }
    return out;
  }
  if (topology === "parallel") {
    return [];
  }
  return pipeline.spec.edges ?? [];
}
