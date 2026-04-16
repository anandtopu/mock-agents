// DAGViewer renders a pipeline topology as a static SVG. Read-only —
// a full editing surface would need a DAG library (React Flow or
// similar) and is a separate slice. We layer nodes left-to-right
// using a simple longest-path-from-source algorithm, group by
// layer for column placement, and stack within a layer by arrival
// order. Works for every current topology:
//
//   - sequential: synthesized edges → one row, N layers
//   - parallel:   no edges → all nodes in layer 0, stacked
//   - graph:      declared edges → real topological layering
//
// The whole thing is a pure function of the inputs, so rendering is
// deterministic and the component stays server-compatible (no
// useState / useEffect needed).

import type { PipelineAgent, PipelineEdge } from "@/lib/api";

interface DAGViewerProps {
  topology: string;
  agents: PipelineAgent[];
  edges: PipelineEdge[];
}

interface Placed {
  agent: PipelineAgent;
  layer: number;
  row: number;
  x: number;
  y: number;
  width: number;
  height: number;
}

const NODE_WIDTH = 160;
const NODE_HEIGHT = 56;
const LAYER_GAP = 80;
const ROW_GAP = 24;
const PADDING = 32;

export function DAGViewer({ topology, agents, edges }: DAGViewerProps) {
  if (agents.length === 0) {
    return (
      <div className="dag-empty">
        This pipeline has no agents. Add entries under{" "}
        <code>spec.agents</code>.
      </div>
    );
  }

  const placement = computeLayout(topology, agents, edges);
  const width =
    (Math.max(...placement.map((p) => p.layer)) + 1) * (NODE_WIDTH + LAYER_GAP) +
    PADDING * 2 -
    LAYER_GAP;
  const height =
    (Math.max(...placement.map((p) => p.row)) + 1) * (NODE_HEIGHT + ROW_GAP) +
    PADDING * 2 -
    ROW_GAP;

  const byId = new Map<string, Placed>(placement.map((p) => [p.agent.id, p]));

  return (
    <div className="dag-wrap">
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={`Pipeline DAG with ${agents.length} agents and ${edges.length} edges`}
      >
        <defs>
          <marker
            id="arrow"
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="6"
            markerHeight="6"
            orient="auto-start-reverse"
          >
            <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
          </marker>
        </defs>

        {/* Edges first so nodes paint over them. */}
        <g className="dag-edges">
          {edges.map((edge, i) => {
            const from = byId.get(edge.from);
            const to = byId.get(edge.to);
            if (!from || !to) return null;
            const x1 = from.x + from.width;
            const y1 = from.y + from.height / 2;
            const x2 = to.x;
            const y2 = to.y + to.height / 2;
            const midX = (x1 + x2) / 2;
            const d = `M ${x1} ${y1} C ${midX} ${y1}, ${midX} ${y2}, ${x2} ${y2}`;
            return (
              <g key={i}>
                <path d={d} fill="none" markerEnd="url(#arrow)" />
                {edge.when_contains && (
                  <text x={midX} y={(y1 + y2) / 2 - 6} textAnchor="middle" className="dag-when">
                    when contains “{edge.when_contains}”
                  </text>
                )}
              </g>
            );
          })}
        </g>

        <g className="dag-nodes">
          {placement.map((p) => (
            <g key={p.agent.id} transform={`translate(${p.x}, ${p.y})`}>
              <rect
                width={p.width}
                height={p.height}
                rx={8}
                ry={8}
                className="dag-node-rect"
              />
              <text x={p.width / 2} y={22} textAnchor="middle" className="dag-node-id">
                {p.agent.id}
              </text>
              <text x={p.width / 2} y={40} textAnchor="middle" className="dag-node-ref">
                {p.agent.ref}
              </text>
            </g>
          ))}
        </g>
      </svg>
    </div>
  );
}

// computeLayout is the layered DAG layout. Returns one Placed entry
// per agent with absolute SVG coordinates.
function computeLayout(
  topology: string,
  agents: PipelineAgent[],
  edges: PipelineEdge[],
): Placed[] {
  const layers = new Map<string, number>();

  if (topology === "parallel") {
    // Every agent at layer 0, one per row.
    return agents.map((agent, i) => ({
      agent,
      layer: 0,
      row: i,
      x: PADDING,
      y: PADDING + i * (NODE_HEIGHT + ROW_GAP),
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
    }));
  }

  // Build adjacency for BFS layer assignment. For sequential
  // topologies the caller already synthesized linear edges.
  const forward = new Map<string, string[]>();
  const inDegree = new Map<string, number>();
  for (const a of agents) {
    forward.set(a.id, []);
    inDegree.set(a.id, 0);
  }
  for (const e of edges) {
    if (!forward.has(e.from) || !forward.has(e.to)) continue;
    forward.get(e.from)!.push(e.to);
    inDegree.set(e.to, (inDegree.get(e.to) ?? 0) + 1);
  }

  // Layer = longest-path-from-any-source. Process nodes in
  // topological order so every predecessor is already layered.
  const queue: string[] = [];
  for (const a of agents) {
    if ((inDegree.get(a.id) ?? 0) === 0) {
      layers.set(a.id, 0);
      queue.push(a.id);
    }
  }
  const remaining = new Map(inDegree);
  while (queue.length > 0) {
    const n = queue.shift()!;
    const here = layers.get(n) ?? 0;
    for (const m of forward.get(n) ?? []) {
      const prev = layers.get(m) ?? 0;
      if (here + 1 > prev) layers.set(m, here + 1);
      const left = (remaining.get(m) ?? 0) - 1;
      remaining.set(m, left);
      if (left === 0) queue.push(m);
    }
  }

  // Any agent not reached via BFS (disconnected cycle or missing
  // source) defaults to layer 0 so it still renders.
  for (const a of agents) {
    if (!layers.has(a.id)) layers.set(a.id, 0);
  }

  // Group by layer, preserve YAML declaration order for the row
  // within each layer so sequential pipelines come out stable.
  const groups = new Map<number, PipelineAgent[]>();
  for (const a of agents) {
    const layer = layers.get(a.id)!;
    if (!groups.has(layer)) groups.set(layer, []);
    groups.get(layer)!.push(a);
  }

  const placed: Placed[] = [];
  for (const [layer, group] of Array.from(groups.entries()).sort((a, b) => a[0] - b[0])) {
    group.forEach((agent, row) => {
      placed.push({
        agent,
        layer,
        row,
        x: PADDING + layer * (NODE_WIDTH + LAYER_GAP),
        y: PADDING + row * (NODE_HEIGHT + ROW_GAP),
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
      });
    });
  }
  return placed;
}
