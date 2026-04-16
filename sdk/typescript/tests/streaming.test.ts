// Streaming-helper tests for the TypeScript SDK. Like client.test.ts
// these run against an in-process http.Server that emits canned SSE
// frames; no real Go binary is spawned. Tests cover both the
// chat/messageStream raw-event surface and the protocol-agnostic
// iterStream + StreamChunk normalization.

import { afterAll, beforeAll, describe, expect as vexpect, it } from "vitest";
import { createServer, Server } from "node:http";
import { AddressInfo } from "node:net";

import {
  MockAgentClient,
  normalizeAnthropicStream,
  normalizeOpenAIStream,
  parseSSEFrame,
} from "../src/client.js";
import { StreamChunk } from "../src/types.js";

let server: Server;
let port: number;

const openaiChunks = [
  `data: ${JSON.stringify({ choices: [{ delta: { content: "a" } }] })}`,
  `data: ${JSON.stringify({ choices: [{ delta: { content: "b" } }] })}`,
  `data: ${JSON.stringify({ choices: [{ delta: {}, finish_reason: "stop" }] })}`,
  `data: [DONE]`,
];

const anthropicEvents = [
  { type: "message_start", message: { model: "claude-x" } },
  { type: "content_block_start", index: 0, content_block: { type: "text" } },
  {
    type: "content_block_delta",
    index: 0,
    delta: { type: "text_delta", text: "hi" },
  },
  { type: "content_block_stop", index: 0 },
  { type: "message_delta", delta: { stop_reason: "end_turn" } },
  { type: "message_stop" },
];

function writeSSE(res: import("node:http").ServerResponse, frames: string[]) {
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
  server = createServer((req, res) => {
    if (req.method === "POST" && req.url === "/v1/chat/completions") {
      writeSSE(res, openaiChunks);
      return;
    }
    if (req.method === "POST" && req.url === "/v1/messages") {
      const frames = anthropicEvents.map(
        (e) => `event: ${e.type}\ndata: ${JSON.stringify(e)}`,
      );
      writeSSE(res, frames);
      return;
    }
    res.writeHead(404);
    res.end();
  });
  await new Promise<void>((resolve) => server.listen(0, resolve));
  port = (server.address() as AddressInfo).port;
});

afterAll(async () => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

// --- parseSSEFrame ---

describe("parseSSEFrame", () => {
  it("extracts event + data lines", () => {
    const got = parseSSEFrame("event: foo\ndata: hello");
    vexpect(got).toEqual({ event: "foo", data: "hello" });
  });

  it("joins multiple data lines with \\n", () => {
    const got = parseSSEFrame("data: line1\ndata: line2");
    vexpect(got).toEqual({ event: "", data: "line1\nline2" });
  });

  it("ignores comment lines", () => {
    const got = parseSSEFrame(": keepalive\ndata: x");
    vexpect(got).toEqual({ event: "", data: "x" });
  });

  it("returns null when no data line is present", () => {
    vexpect(parseSSEFrame("event: foo")).toBeNull();
  });
});

// --- raw streaming via http server ---

describe("MockAgentClient.chatStream", () => {
  it("yields parsed chunks and stops on [DONE]", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const events: any[] = [];
    for await (const chunk of client.chatStream([{ role: "user", content: "x" }])) {
      events.push(chunk);
    }
    vexpect(events.length).toBe(3);
    vexpect(events[0].choices[0].delta.content).toBe("a");
    vexpect(events[2].choices[0].finish_reason).toBe("stop");
  });
});

describe("MockAgentClient.messageStream", () => {
  it("yields anthropic events and stops on message_stop", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const events: any[] = [];
    for await (const event of client.messageStream([{ role: "user", content: "x" }])) {
      events.push(event);
    }
    const types = events.map((e) => e.type);
    vexpect(types).toEqual([
      "message_start",
      "content_block_start",
      "content_block_delta",
      "content_block_stop",
      "message_delta",
      "message_stop",
    ]);
  });
});

// --- iterStream end-to-end ---

describe("MockAgentClient.iterStream", () => {
  it("normalizes openai chunks to StreamChunks", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const chunks: StreamChunk[] = [];
    for await (const chunk of client.iterStream([{ role: "user", content: "x" }], {
      protocol: "openai",
    })) {
      chunks.push(chunk);
    }
    const text = chunks.map((c) => c.text).join("");
    vexpect(text).toBe("ab");
    vexpect(chunks[chunks.length - 1].finished).toBe(true);
    vexpect(chunks[chunks.length - 1].finishReason).toBe("stop");
  });

  it("normalizes anthropic events to StreamChunks", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    const chunks: StreamChunk[] = [];
    for await (const chunk of client.iterStream([{ role: "user", content: "x" }], {
      protocol: "anthropic",
    })) {
      chunks.push(chunk);
    }
    const text = chunks.map((c) => c.text).join("");
    vexpect(text).toBe("hi");
    vexpect(chunks[chunks.length - 1].finished).toBe(true);
    vexpect(chunks[chunks.length - 1].finishReason).toBe("end_turn");
  });

  it("rejects unknown protocol", async () => {
    const client = new MockAgentClient({ baseUrl: `http://localhost:${port}` });
    await vexpect(async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const it = client.iterStream([], { protocol: "bogus" } as any);
      await it.next();
    }).rejects.toThrow(/unknown protocol/);
  });
});

// --- normalizer unit tests against constructed inputs ---

async function* fromArray<T>(items: T[]): AsyncGenerator<T> {
  for (const item of items) yield item;
}

describe("normalizeOpenAIStream", () => {
  it("filters padding chunks and emits tool call deltas", async () => {
    const raw = [
      { choices: [{ delta: {} }] }, // padding -> skipped
      { choices: [{ delta: { content: "x" } }] },
      {
        choices: [
          {
            delta: {
              tool_calls: [
                { index: 0, function: { name: "search", arguments: '{"q":' } },
              ],
            },
          },
        ],
      },
      { choices: [{ delta: {}, finish_reason: "tool_calls" }] },
    ];
    const out: StreamChunk[] = [];
    for await (const c of normalizeOpenAIStream(fromArray(raw))) out.push(c);
    vexpect(out.length).toBe(3);
    vexpect(out[0].text).toBe("x");
    vexpect(out[1].toolCallDelta).toEqual([0, "search", '{"q":']);
    vexpect(out[2].finished).toBe(true);
    vexpect(out[2].finishReason).toBe("tool_calls");
  });
});

describe("normalizeAnthropicStream", () => {
  it("emits text + finish for a text-only stream", async () => {
    const events = [
      { type: "message_start", message: {} },
      { type: "content_block_start", index: 0, content_block: { type: "text" } },
      {
        type: "content_block_delta",
        delta: { type: "text_delta", text: "hello " },
      },
      {
        type: "content_block_delta",
        delta: { type: "text_delta", text: "world" },
      },
      { type: "message_delta", delta: { stop_reason: "end_turn" } },
      { type: "message_stop" },
    ];
    const out: StreamChunk[] = [];
    for await (const c of normalizeAnthropicStream(fromArray(events))) out.push(c);
    const text = out.map((c) => c.text).join("");
    vexpect(text).toBe("hello world");
    vexpect(out[out.length - 1].finished).toBe(true);
    vexpect(out[out.length - 1].finishReason).toBe("end_turn");
  });

  it("accumulates input_json_delta fragments under the active tool", async () => {
    const events = [
      {
        type: "content_block_start",
        index: 0,
        content_block: { type: "tool_use", name: "get_weather" },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "input_json_delta", partial_json: '{"city":' },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "input_json_delta", partial_json: '"Tokyo"}' },
      },
      { type: "message_stop" },
    ];
    const out: StreamChunk[] = [];
    for await (const c of normalizeAnthropicStream(fromArray(events))) out.push(c);
    const fragments = out
      .filter((c) => c.toolCallDelta && c.toolCallDelta[2].length > 0)
      .map((c) => c.toolCallDelta![2])
      .join("");
    vexpect(fragments).toBe('{"city":"Tokyo"}');
    vexpect(out.find((c) => c.toolCallDelta && c.toolCallDelta[1] === "get_weather")).toBeTruthy();
    vexpect(out[out.length - 1].finished).toBe(true);
  });

  it("ignores events past message_stop", async () => {
    const events = [
      { type: "message_stop" },
      {
        type: "content_block_delta",
        delta: { type: "text_delta", text: "leak" },
      },
    ];
    const out: StreamChunk[] = [];
    for await (const c of normalizeAnthropicStream(fromArray(events))) out.push(c);
    vexpect(out.length).toBe(1);
    vexpect(out[0].finished).toBe(true);
  });
});
