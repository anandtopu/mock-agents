// Same-origin SSE proxy for the live log feed. The browser cannot
// send an Authorization header on an EventSource connection, so we
// proxy through this Next.js route — server-side we read the
// auth cookie and forward it as a Bearer token when hitting the
// upstream /api/v1/logs/stream endpoint.
//
// The response body is piped straight through without re-framing so
// every `event:` / `data:` line the backend emits reaches the client
// intact. Disconnects propagate both ways: the client closes the
// EventSource → AbortController → upstream request cancels.

import { NextRequest } from "next/server";

import { getAuthKey, getBaseUrl } from "@/lib/api";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  const upstream = `${getBaseUrl()}/api/v1/logs/stream`;
  const key = await getAuthKey();
  const headers: Record<string, string> = { Accept: "text/event-stream" };
  if (key) headers.Authorization = `Bearer ${key}`;

  // Tie upstream lifetime to the browser's EventSource. When the
  // client aborts, req.signal fires and the fetch cancels — that
  // closes the backend handler's request context on the Go side.
  let upstreamResp: Response;
  try {
    upstreamResp = await fetch(upstream, {
      headers,
      signal: req.signal,
      // next/fetch caches by default; streams must opt out.
      cache: "no-store",
    });
  } catch (err) {
    const message = err instanceof Error ? err.message : "upstream unreachable";
    return new Response(`upstream fetch failed: ${message}`, { status: 502 });
  }

  if (!upstreamResp.ok || !upstreamResp.body) {
    const body = await upstreamResp.text().catch(() => "");
    return new Response(body || `upstream returned ${upstreamResp.status}`, {
      status: upstreamResp.status,
    });
  }

  return new Response(upstreamResp.body, {
    status: 200,
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}
