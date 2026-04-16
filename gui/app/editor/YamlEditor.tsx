"use client";

// YamlEditor is the client island for /editor. It renders a big
// textarea, a Validate button that POSTs to the server-action
// `validateAction`, and an errors panel below. Server-side
// validation keeps the schema rules and the CLI in lockstep — no
// JSON-schema-in-the-browser, no ajv dep, no divergence.
//
// The textarea is intentionally plain. A full Monaco drop-in would
// add ~3 MB of bundle for features (autocomplete, folding) that
// most operators will never use from the GUI. Line numbers and
// error markers are rendered as a sibling column so the user still
// gets inline feedback without the editor widget.

import { useMemo, useState, useTransition } from "react";

import type { ValidateResult, ValidationError } from "@/lib/api";

const SAMPLE_AGENT = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: hello-world
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        match:
          default: true
        response:
          content: "Hello from MockAgents"
`;

interface YamlEditorProps {
  validateAction: (yaml: string) => Promise<ValidateResult>;
}

export function YamlEditor({ validateAction }: YamlEditorProps) {
  const [yaml, setYaml] = useState(SAMPLE_AGENT);
  const [result, setResult] = useState<ValidateResult | null>(null);
  const [isPending, startTransition] = useTransition();

  const lineCount = useMemo(() => yaml.split("\n").length, [yaml]);

  function onValidate() {
    startTransition(async () => {
      const r = await validateAction(yaml);
      setResult(r);
    });
  }

  return (
    <div className="editor-layout">
      <div className="editor-toolbar">
        <button
          type="button"
          className="btn btn-primary"
          onClick={onValidate}
          disabled={isPending}
        >
          {isPending ? "Validating…" : "Validate"}
        </button>
        <button
          type="button"
          className="btn"
          onClick={() => setYaml(SAMPLE_AGENT)}
          disabled={isPending}
        >
          Reset to sample
        </button>
        <span className="muted editor-meta">
          {lineCount} lines · {yaml.length} chars
          {result && <> · kind: <code>{result.kind || "?"}</code></>}
        </span>
      </div>

      <div className="editor-grid">
        <pre className="editor-gutter" aria-hidden="true">
          {Array.from({ length: lineCount }, (_, i) => i + 1).join("\n")}
        </pre>
        <textarea
          className="editor-textarea"
          spellCheck={false}
          autoCorrect="off"
          autoCapitalize="off"
          value={yaml}
          onChange={(e) => setYaml(e.target.value)}
          aria-label="Agent YAML"
        />
      </div>

      {result && <ResultPanel result={result} />}
    </div>
  );
}

function ResultPanel({ result }: { result: ValidateResult }) {
  if (result.ok) {
    return (
      <div className="banner banner-ok">
        <strong>Valid.</strong> Kind: <code>{result.kind}</code>. The document
        passed every schema and cross-reference check.
      </div>
    );
  }
  return (
    <div className="banner banner-error editor-errors">
      <strong>{result.errors.length} error{result.errors.length === 1 ? "" : "s"}.</strong>
      <ul>
        {result.errors.map((err, i) => (
          <li key={i}>
            <ErrorRow err={err} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function ErrorRow({ err }: { err: ValidationError }) {
  return (
    <div>
      <div>
        {err.line ? <code>line {err.line}</code> : null}
        {err.line && err.field ? " · " : null}
        {err.field && <code>{err.field}</code>}
        <span> — {err.message}</span>
      </div>
      {err.suggestion && <div className="muted">Suggestion: {err.suggestion}</div>}
    </div>
  );
}
