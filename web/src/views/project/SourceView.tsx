import { useState } from "react";
import { useProject } from "../../layouts/ProjectLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { request } from "../../services/api";
import { sourceData } from "../../lib/project";
import { SourceFields } from "../../components/SourceFields";
import { ProjectHeading } from "../../components/ProjectHeading";
import { Field } from "../../components/ui";

export function SourceView() {
  const { project, refresh } = useProject(),
    { can } = useSession(),
    ui = useUI(),
    [reveal, setReveal] = useState(false);
  const edit = () =>
    ui.dialog({
      title: "Project source",
      description:
        "One repository, branch, and credential set for this project. Changes apply to the whole project on its next deployment.",
      fields: <SourceFields project={project} />,
      submit: "Save project source",
      onSubmit: async (form) => {
        await request(`projects/${project.id}`, sourceData(form), "PATCH");
        await refresh();
        ui.notify("Project source saved");
      },
    });
  return (
    <>
      <ProjectHeading title="Source" />
      <div className="integration-layout">
        <section className="panel panel-pad">
          <div className="section-head">
            <h2>Project source</h2>
            <button disabled={!can("project.source", project)} onClick={edit}>
              Edit source
            </button>
          </div>
          <p className="small muted">
            A single repository and branch for every container.
          </p>
          <dl className="source-details">
            <dt>Repository</dt>
            <dd>{project.git_url || "Not connected"}</dd>
            <dt>Branch</dt>
            <dd>{project.branch || "main"}</dd>
            <dt>Credentials</dt>
            <dd>{project.has_git_token ? "Token configured" : "No token"}</dd>
          </dl>
        </section>
        <section className="panel panel-pad">
          <h2>GitHub webhook</h2>
          <p className="small muted">
            A signed push queues one project deployment when automatic
            deployment is enabled in the project overview.
          </p>
          <Field
            label="Payload URL"
            readOnly
            value={
              project.webhook_secret
                ? `${location.origin}/api/webhooks/github/${project.id}`
                : ""
            }
          />
          <Field
            label="Webhook secret"
            readOnly
            type={reveal ? "text" : "password"}
            value={project.webhook_secret || "Connect a source first"}
          />
          <button onClick={() => setReveal(!reveal)}>
            {reveal ? "Hide secret" : "Show secret"}
          </button>
          <p className="small muted">
            Content type: application/json · Event: push
          </p>
        </section>
      </div>
    </>
  );
}
