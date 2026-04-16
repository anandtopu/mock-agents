// Server-side proxy that re-exposes /api/v1/logs at the GUI's own
// origin so the AutoRefreshLogs client island can poll without
// running into CORS or needing to know the upstream MockAgents URL.
//
// This is intentionally a thin pass-through — query params forwarded
// verbatim, response body relayed unchanged. We do not cache because
// the whole point of polling is to see new rows as they appear.

import { NextRequest, NextResponse } from "next/server";

import { listLogs } from "@/lib/api";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  const params = req.nextUrl.searchParams;
  const limit = params.get("limit");
  const agent = params.get("agent");
  const since = params.get("since");
  try {
    const logs = await listLogs({
      limit: limit ? Number(limit) : undefined,
      agent: agent ?? undefined,
      since: since ?? undefined,
    });
    return NextResponse.json(logs);
  } catch (err) {
    const message = err instanceof Error ? err.message : "unknown error";
    return NextResponse.json({ error: message }, { status: 502 });
  }
}
