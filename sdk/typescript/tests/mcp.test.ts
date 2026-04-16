// MCP bidirectional helper tests. Like streaming.test.ts these run
// against an in-process node:http server that serves canned SSE
// frames on GET /mcp/events and collects POST /mcp/response bodies
// for later inspection.

import {
  afterAll,
  beforeAll,
  beforeEach,
  describe,
  expect as vexpect,
  it,
} from "vitest";
import { createServer, Server, IncomingMessage, ServerResponse } from "node:http";
import { AddressInfo } from "node:net";

import {
  McpClient,
  McpEvent,
  isRequest,
  paramsOf,
  parseMcpFrame,
} from "../src/mcp.js";

interface Recorded {
  method: string;
  url: string;
  body: string;
}

let server: Server;
let port: number;

// Canned /mcp/events frames. Each test can overwrite this before
// calling connect() to drive a different scenario.
let eventsFrames: string[] = [];

// Every POST /mcp/response body we saw during the test.
let recorded: Recorded[] = [];

// When set, the /mcp/response endpoint returns this status instead
// of 202 — used to exercise the error path.
let responseStatus = 202;

function writeSSE(res: ServerResponse, frames: string[]) {
  res.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  });
  for (const frame of frames) {
    res.write(frame + "\n\n");
  }
  res.end();
}

beforeAll(async () => {
  server = createServer((req: IncomingMessage, res: ServerResponse) => {
    if (req.method === "GET" && req.url === "/mcp/events") {
      writeSSE(res, eventsFrames);
      return;
    }
    if (req.method === "POST" && req.url === "/mcp/response") {
      let body = "";
      req.on("data", (chunk) => (body += chunk.toString()));
      req.on("end", () => {
        recorded.push({ method: req.method!, url: req.url!, body });
        res.writeHead(responseStatus);
        res.end();
      });
      return;
    }
    res.writeHead(404);
    res.end("not found");
  });
  await new Promise<void>((resolve) => server.listen(0, resolve));
  port = (server.address() as AddressInfo).port;
});

afterAll(async () => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

beforeEach(() => {
  eventsFrames = [];
  recorded = [];
  responseStatus = 202;
});

function baseUrl(): string {
  return `http://127.0.0.1:${port}`;
}

async function collectEvents(client: McpClient): Promise<McpEvent[]> {
  const stream = client.connect();
  const out: McpEvent[] = [];
  for await (const event of stream) {
    out.push(event);
  }
  await stream.close();
  return out;
}

// --- parseMcpFrame unit tests (no HTTP) ---

describe("parseMcpFrame", () => {
  it("decodes a request frame", () => {
    const ev = parseMcpFrame(
      'event: request\ndata: {"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{"prompt":"hi"}}',
    );
    vexpect(ev).not.toBeNull();
    vexpect(ev!.kind).toBe("request");
    vexpect(ev!.payload.id).toBe(1);
    vexpect(ev!.payload.method).toBe("sampling/createMessage");
  });

  it("decodes a notification frame", () => {
    const ev = parseMcpFrame(
      'event: notification\ndata: {"jsonrpc":"2.0","method":"notifications/tools/list_changed"}',
    );
    vexpect(ev).not.toBeNull();
    vexpect(ev!.kind).toBe("notification");
    vexpect(ev!.payload.id).toBeUndefined();
  });

  it("drops frames with malformed data", () => {
    const ev = parseMcpFrame("event: request\ndata: not-json");
    vexpect(ev).toBeNull();
  });

  it("drops frames with no data line", () => {
    const ev = parseMcpFrame("event: request\n");
    vexpect(ev).toBeNull();
  });
});

// --- isRequest / paramsOf accessors ---

describe("McpEvent accessors", () => {
  it("isRequest true only with an id", () => {
    vexpect(
      isRequest({
        kind: "request",
        payload: { jsonrpc: "2.0", id: 1, method: "m" },
      }),
    ).toBe(true);
    vexpect(
      isRequest({ kind: "request", payload: { jsonrpc: "2.0", method: "m" } }),
    ).toBe(false);
    vexpect(
      isRequest({
        kind: "notification",
        payload: { jsonrpc: "2.0", method: "m" },
      }),
    ).toBe(false);
  });

  it("paramsOf always returns a dict", () => {
    vexpect(
      paramsOf({ kind: "request", payload: { jsonrpc: "2.0", method: "m" } }),
    ).toEqual({});
    vexpect(
      paramsOf({
        kind: "request",
        payload: { jsonrpc: "2.0", method: "m", params: ["oops"] },
      }),
    ).toEqual({});
    vexpect(
      paramsOf({
        kind: "request",
        payload: { jsonrpc: "2.0", method: "m", params: { a: 1 } },
      }),
    ).toEqual({ a: 1 });
  });
});

// --- end-to-end against the canned server ---

describe("McpClient against node:http server", () => {
  it("parses a request frame from /mcp/events", async () => {
    eventsFrames = [
      'event: request\ndata: {"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{"prompt":"hi"}}',
    ];
    const client = new McpClient({ baseUrl: baseUrl() });
    const events = await collectEvents(client);
    vexpect(events).toHaveLength(1);
    vexpect(events[0].kind).toBe("request");
    vexpect(events[0].payload.method).toBe("sampling/createMessage");
  });

  it("skips heartbeats and malformed frames", async () => {
    eventsFrames = [
      ":heartbeat",
      "event: request\ndata: not-json",
      'event: request\ndata: {"jsonrpc":"2.0","id":2,"method":"roots/list"}',
    ];
    const client = new McpClient({ baseUrl: baseUrl() });
    const events = await collectEvents(client);
    vexpect(events).toHaveLength(1);
    vexpect(events[0].payload.method).toBe("roots/list");
  });

  it("sendResponse posts the expected JSON body", async () => {
    const client = new McpClient({ baseUrl: baseUrl() });
    await client.sendResponse(7, { result: { text: "pong" } });
    vexpect(recorded).toHaveLength(1);
    const body = JSON.parse(recorded[0].body);
    vexpect(body).toEqual({
      jsonrpc: "2.0",
      id: 7,
      result: { text: "pong" },
    });
  });

  it("sendResponse rejects when neither result nor error is supplied", async () => {
    const client = new McpClient({ baseUrl: baseUrl() });
    await vexpect(client.sendResponse(1, {})).rejects.toThrow(/exactly one/);
  });

  it("sendResponse throws HTTPError on non-2xx", async () => {
    responseStatus = 404;
    const client = new McpClient({ baseUrl: baseUrl() });
    await vexpect(
      client.sendResponse(999, { result: {} }),
    ).rejects.toThrow(/404/);
  });

  it("dispatchRequest routes to handler and posts the result", async () => {
    const client = new McpClient({ baseUrl: baseUrl() });
    const event: McpEvent = {
      kind: "request",
      payload: {
        jsonrpc: "2.0",
        id: 42,
        method: "sampling/createMessage",
        params: { prompt: "hi" },
      },
    };
    let seen: Record<string, unknown> | null = null;
    const result = await client.dispatchRequest(event, {
      "sampling/createMessage": (params) => {
        seen = params;
        return { text: "ok" };
      },
    });
    vexpect(result).toEqual({ text: "ok" });
    vexpect(seen).toEqual({ prompt: "hi" });
    vexpect(recorded).toHaveLength(1);
    const body = JSON.parse(recorded[0].body);
    vexpect(body.id).toBe(42);
    vexpect(body.result).toEqual({ text: "ok" });
  });

  it("dispatchRequest posts -32601 for unknown method and throws", async () => {
    const client = new McpClient({ baseUrl: baseUrl() });
    const event: McpEvent = {
      kind: "request",
      payload: { jsonrpc: "2.0", id: 1, method: "unknown/method", params: {} },
    };
    await vexpect(client.dispatchRequest(event, {})).rejects.toThrow(
      "unknown/method",
    );
    const body = JSON.parse(recorded[0].body);
    vexpect(body.error.code).toBe(-32601);
  });

  it("dispatchRequest posts -32603 and rethrows when the handler throws", async () => {
    const client = new McpClient({ baseUrl: baseUrl() });
    const event: McpEvent = {
      kind: "request",
      payload: { jsonrpc: "2.0", id: 9, method: "sampling/createMessage", params: {} },
    };
    await vexpect(
      client.dispatchRequest(event, {
        "sampling/createMessage": () => {
          throw new Error("kaboom");
        },
      }),
    ).rejects.toThrow("kaboom");
    const body = JSON.parse(recorded[0].body);
    vexpect(body.error.code).toBe(-32603);
    vexpect(body.error.message).toContain("kaboom");
  });

  it("dispatchRequest rejects non-request events", async () => {
    const client = new McpClient({ baseUrl: baseUrl() });
    const event: McpEvent = {
      kind: "notification",
      payload: { jsonrpc: "2.0", method: "notifications/tools/list_changed" },
    };
    await vexpect(client.dispatchRequest(event, {})).rejects.toThrow(/non-request/);
  });
});
