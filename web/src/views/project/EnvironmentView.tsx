import { useProject } from "../../layouts/ProjectLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { request } from "../../services/api";
import { environmentData, text } from "../../lib/project";
import { ProjectHeading } from "../../components/ProjectHeading";
import { AsyncForm } from "../../components/ui";

export function EnvironmentView() {
  const { project, refresh } = useProject(),
    { can } = useSession(),
    ui = useUI();
  const editable = can("project.environment", project);
  return (
    <>
      <ProjectHeading title="Environment" />
      {!editable && (
        <p className="note locked-note">
          Your roles do not let you change the environment.
        </p>
      )}
      <AsyncForm
        key={project.id}
        className="panel panel-pad project-form"
        disabled={!editable}
        onSubmit={async (form) => {
          await request(
            `projects/${project.id}`,
            { env: environmentData(text(form, "env")) },
            "PATCH",
          );
          await refresh();
          ui.notify("Environment saved; changes apply on next deployment");
        }}
      >
        <div className="section-head">
          <h2>Environment</h2>
          <span className="small muted">Project-wide configuration</span>
        </div>
        <p className="small muted">
          Applied to every container on the next deployment. PORT is assigned
          automatically unless overridden.
        </p>
        <label>
          Variables
          <textarea
            name="env"
            rows={14}
            spellCheck={false}
            placeholder="NODE_ENV=production"
            defaultValue={Object.entries(project.env || {})
              .map(([key, value]) => `${key}=${value}`)
              .join("\n")}
          />
        </label>
        <button className="primary">Save environment</button>
      </AsyncForm>
    </>
  );
}
