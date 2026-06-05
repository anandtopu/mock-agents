import Link from "next/link";
import { notFound } from "next/navigation";

import {
  getPipelineWithVersion,
  listAgents,
  savePipeline,
  validateYAML,
  type PipelineDefinition,
  type SavePipelineResult,
  type ValidateResult,
} from "@/lib/api";
import { Icon } from "@/lib/icons";
import { PipelineEditor } from "./PipelineEditor";

type PageProps = {
  params: Promise<{ name: string }>;
};

export default async function PipelineEditPage({ params }: PageProps) {
  const { name } = await params;
  const loaded = await getPipelineWithVersion(name);
  if (!loaded) notFound();

  // Agent names populate the node ref picker. Failing to list agents (e.g. a
  // permission error) degrades to an empty picker rather than blocking edits.
  let agentNames: string[] = [];
  try {
    agentNames = (await listAgents()).map((a) => a.name).sort();
  } catch {
    agentNames = [];
  }

  // Both actions run server-side so the auth cookie is threaded into the
  // upstream fetch. The client editor calls validateAction (debounced) and
  // saveAction (on Save).
  async function validateAction(json: string): Promise<ValidateResult> {
    "use server";
    try {
      return await validateYAML(json);
    } catch (err) {
      const message = err instanceof Error ? err.message : "unknown error";
      return {
        ok: false,
        kind: "",
        errors: [{ field: "transport", message: `Server unreachable: ${message}` }],
      };
    }
  }

  async function saveAction(
    definition: PipelineDefinition,
    version: string,
  ): Promise<SavePipelineResult> {
    "use server";
    return savePipeline(name, definition, version);
  }

  return (
    <div>
      <Link
        href={`/pipelines/${encodeURIComponent(name)}`}
        className="btn btn-ghost btn-sm"
        style={{ marginLeft: -8, marginBottom: 10 }}
      >
        <Icon name="arrow-left" size={15} /> {name}
      </Link>

      <div className="head-row mb-4">
        <div className="agent-icon" style={{ width: 44, height: 44, flex: "0 0 44px" }}>
          <Icon name="workflow" size={22} />
        </div>
        <div className="grow">
          <div className="row gap-3" style={{ flexWrap: "wrap" }}>
            <h1 className="page-title">Edit {name}</h1>
            <span className="badge badge-outline">{loaded.definition.spec.topology}</span>
          </div>
          <p className="page-lede" style={{ marginTop: 8 }}>
            Drag nodes to rearrange, and in <code>graph</code> topology connect handles to rewire.
          </p>
        </div>
      </div>

      <PipelineEditor
        pipeline={loaded.definition}
        agentNames={agentNames}
        version={loaded.version}
        validateAction={validateAction}
        saveAction={saveAction}
      />
    </div>
  );
}
