import { APIError, CostGroup, getCosts } from "@/lib/api";

export const dynamic = "force-dynamic";

export default async function CostsPage() {
  let costs: Awaited<ReturnType<typeof getCosts>> = null;
  let error: string | null = null;
  try {
    costs = await getCosts({ limit: 1000 });
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  return (
    <div>
      <h1 className="page-title">Cost estimates</h1>
      <p className="page-lede">
        Aggregated from the interaction log using the configured pricing
        table. Set <code>MOCKAGENTS_PRICING</code> to override the
        defaults shipped in <code>internal/pricing/pricing.go</code>.
      </p>

      {error && (
        <div className="banner banner-error">
          <strong>Could not load costs.</strong> {error}
        </div>
      )}

      {!error && costs === null && (
        <div className="empty">
          Cost aggregation requires the interaction log store to be
          enabled. Restart the server with <code>--log-db</code>.
        </div>
      )}

      {costs && (
        <>
          <div className="cost-totals">
            <div className="cost-tile">
              <dt>Total requests</dt>
              <dd>{costs.total_requests.toLocaleString()}</dd>
            </div>
            <div className="cost-tile">
              <dt>Prompt tokens</dt>
              <dd>{costs.total_prompt_tokens.toLocaleString()}</dd>
            </div>
            <div className="cost-tile">
              <dt>Completion tokens</dt>
              <dd>{costs.total_completion_tokens.toLocaleString()}</dd>
            </div>
            <div className="cost-tile cost-tile-headline">
              <dt>Estimated cost</dt>
              <dd>{formatUSD(costs.total_cost_usd)}</dd>
            </div>
          </div>

          <h2 className="section-title">By model</h2>
          <CostTable rows={costs.by_model} keyLabel="Model" />

          <h2 className="section-title">By agent</h2>
          <CostTable rows={costs.by_agent} keyLabel="Agent" />
        </>
      )}
    </div>
  );
}

function CostTable({ rows, keyLabel }: { rows: CostGroup[]; keyLabel: string }) {
  if (rows.length === 0) {
    return <p className="muted">No data in the current window.</p>;
  }
  return (
    <div className="table-wrap">
      <table className="cost-table">
        <thead>
          <tr>
            <th>{keyLabel}</th>
            <th className="right">Requests</th>
            <th className="right">Prompt tok</th>
            <th className="right">Completion tok</th>
            <th className="right">Cost (USD)</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.key}>
              <td>
                <code>{row.key}</code>
              </td>
              <td className="right">{row.requests.toLocaleString()}</td>
              <td className="right">{row.prompt_tokens.toLocaleString()}</td>
              <td className="right">{row.completion_tokens.toLocaleString()}</td>
              <td className="right">{formatUSD(row.cost_usd)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function formatUSD(amount: number): string {
  // Estimates are typically < $1 in dev, so always show 4 decimals so
  // a $0.0023 row doesn't render as "$0.00".
  return `$${amount.toFixed(4)}`;
}
