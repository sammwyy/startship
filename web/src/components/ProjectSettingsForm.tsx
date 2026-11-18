import { Link } from "react-router-dom";
import { useProject } from "../layouts/ProjectLayout";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { request } from "../services/api";
import { text } from "../lib/project";
import { AsyncForm, Field } from "./ui";

export function ProjectSettingsForm() {
  const { project, refresh } = useProject(),
    { can } = useSession(),
    ui = useUI();
  const editable = can("project.settings", project);
  return (
    <>
      {!editable && (
        <p className="note locked-note">
          You can view this section, but your roles do not let you change it.
        </p>
      )}
      <AsyncForm
        key={project.id}
        className="panel panel-pad project-form"
        disabled={!editable}
        onSubmit={async (form) => {
          await request(
            `projects/${project.id}`,
            {
              name: text(form, "name"),
              root_dir: text(form, "root_dir"),
              auto_deploy: form.has("auto_deploy"),
            },
            "PATCH",
          );
          await refresh();
          ui.notify("Project saved; changes apply on next deployment");
        }}
      >
        <div className="section-head">
          <div>
            <h2>General configuration</h2>
            <p className="small">Shared by every container in this project.</p>
          </div>
        </div>
        <div className="split">
          <Field
            label="Project name"
            name="name"
            defaultValue={project.name}
            required
          />
          <Field
            label="Repository root"
            name="root_dir"
            defaultValue={project.root_dir || "."}
            required
          />
        </div>
        <label className="inline-check">
          <input
            type="checkbox"
            name="auto_deploy"
            defaultChecked={project.auto_deploy}
          />
          Deploy project on source pushes
        </label>
        <p className="small muted">
          Images, commands and health checks live in the{" "}
          <Link to={`/project/${project.id}/pipeline`}>Pipeline section</Link>.
        </p>
        <div className="configuration-actions">
          <span>Applies on the next deployment.</span>
          <button className="primary">Save changes</button>
        </div>
      </AsyncForm>
    </>
  );
}
