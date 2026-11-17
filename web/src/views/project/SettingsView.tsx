import { useProject } from "../../layouts/ProjectLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { request } from "../../services/api";
import { text } from "../../lib/project";
import { ProjectHeading } from "../../components/ProjectHeading";
import { AsyncForm, Field } from "../../components/ui";

export function SettingsView() {
  const { project, refresh } = useProject(),
    { can } = useSession(),
    ui = useUI();
  const editable = can("project.settings", project);
  return (
    <>
      <ProjectHeading title="Settings" />
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
              build_type: text(form, "build_type"),
              port: Number(form.get("port")),
              test_command: text(form, "test_command"),
              auto_deploy: form.has("auto_deploy"),
            },
            "PATCH",
          );
          await refresh();
          ui.notify("Project saved; changes apply on next deployment");
        }}
      >
        <div className="section-head">
          <h2>Project settings</h2>
          <span className="small muted">Shared by every container</span>
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
        <div className="split">
          <label>
            Build strategy
            <select
              name="build_type"
              defaultValue={project.build_type || "auto"}
            >
              {[
                "auto",
                "dockerfile",
                "static",
                "node",
                "python",
                "go",
                "rust",
              ].map((t) => (
                <option key={t}>{t}</option>
              ))}
            </select>
          </label>
          <Field
            label="HTTP port override (0 = detect)"
            name="port"
            type="number"
            min={0}
            max={65535}
            defaultValue={project.port || 0}
            required
          />
        </div>
        <Field
          label="Test command (optional, run in each detected container)"
          name="test_command"
          defaultValue={project.test_command || ""}
        />
        <label className="inline-check">
          <input
            type="checkbox"
            name="auto_deploy"
            defaultChecked={project.auto_deploy}
          />
          Deploy project on source pushes
        </label>
        <p className="note">
          Auto detects one server at the repository root, or multiple server
          folders in a monorepo. A manual build strategy treats the selected
          root as one container. Changes apply together on the next deployment.
        </p>
        <button className="primary">Save project settings</button>
      </AsyncForm>
    </>
  );
}
