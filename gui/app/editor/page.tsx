import { YamlEditor } from "./YamlEditor";
import { validateYAML, ValidateResult } from "@/lib/api";

export default function EditorPage() {
  // The "validate" action runs server-side so we get the auth
  // cookie threaded into the upstream fetch for free. The client
  // component calls this via a useActionState form submission.
  async function validateAction(yaml: string): Promise<ValidateResult> {
    "use server";
    try {
      return await validateYAML(yaml);
    } catch (err) {
      const message = err instanceof Error ? err.message : "unknown error";
      return {
        ok: false,
        kind: "",
        errors: [
          {
            field: "transport",
            message: `Server unreachable: ${message}`,
          },
        ],
      };
    }
  }

  return (
    <div>
      <h1 className="page-title">Config editor</h1>
      <p className="page-lede">
        Paste an agent definition below and click <strong>Validate</strong>.
        The server runs the same validator as <code>mockagents validate</code>
        and returns inline errors. This is a validation playground — it does
        not persist changes. Edit the file on disk and restart
        <code> mockagents start</code> (or rely on hot reload) to apply.
      </p>
      <YamlEditor validateAction={validateAction} />
    </div>
  );
}
