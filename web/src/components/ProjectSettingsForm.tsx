import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useProject } from "../layouts/ProjectLayout";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { request } from "../services/api";
import { text } from "../lib/project";
import { AsyncForm, Field } from "./ui";

export function ProjectSettingsForm() {
  const { project, containers, refresh } = useProject(),
    { can } = useSession(),
    ui = useUI();
  const editable = can("project.settings", project);
  const staticOnly =
    containers.length > 0 && containers.every((c) => c.pipeline?.deploy.static);
  const [sleepEnabled, setSleepEnabled] = useState(!!project.sleep_after);
  useEffect(() => {
    setSleepEnabled(!!project.sleep_after);
  }, [project.id, project.sleep_after]);
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
              previews: form.has("previews"),
              sleep_after:
                !staticOnly && form.has("sleep_enabled")
                  ? Number(text(form, "sleep_after"))
                  : 0,
            },
            "PATCH",
          );
          await refresh();
          ui.notify("Project saved; idle sleep settings apply immediately");
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
        <div className="idle-sleep-settings">
          <label className="inline-check">
            <input
              type="checkbox"
              name="previews"
              defaultChecked={project.previews}
              aria-describedby="branch-previews-help"
            />
            Branch previews
          </label>
          <p className="small muted" id="branch-previews-help">
            Pushes to other branches deploy at &lt;branch&gt;--&lt;host&gt;;
            removed when deleted or merged, and sleep when idle.
          </p>
        </div>
        <div className="idle-sleep-settings">
          <div className="idle-sleep-control">
            <label className="inline-check">
              <input
                type="checkbox"
                name="sleep_enabled"
                checked={!staticOnly && sleepEnabled}
                disabled={staticOnly}
                onChange={(event) => setSleepEnabled(event.target.checked)}
                aria-describedby="idle-sleep-help"
              />
              Sleep when idle
            </label>
            {!staticOnly && sleepEnabled && (
              <label className="idle-sleep-minutes">
                <input
                  type="number"
                  name="sleep_after"
                  aria-label="Idle minutes"
                  min={1}
                  max={10080}
                  defaultValue={project.sleep_after || 15}
                  required
                />
                <span className="muted">min</span>
              </label>
            )}
          </div>
          {staticOnly && (
            <p className="small muted">
              Static-only projects have no runtime containers to stop.
            </p>
          )}
          <p className="small muted" id="idle-sleep-help">
            Stops the project's containers after this long without requests; the
            next request wakes them (first request waits for the health check).
          </p>
        </div>
        <p className="small muted">
          Images, commands and health checks live in the{" "}
          <Link to={`/project/${project.id}/pipeline`}>Pipeline section</Link>.
        </p>
        <div className="configuration-actions">
          <span>
            Idle sleep applies immediately; repository root on next deployment.
          </span>
          <button className="primary">Save changes</button>
        </div>
      </AsyncForm>
    </>
  );
}
