import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { useDashboard } from "../layouts/DashboardLayout";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { request } from "../services/api";
import { queries } from "../services/queries";
import { text } from "../lib/project";
import { Heading } from "../components/Heading";
import { AsyncForm, ErrorMessage, Field, Loading } from "../components/ui";
import { useLiveFeed, useLiveSubscription } from "../providers/LiveProvider";
import type { Project } from "../services/types";

interface StatusSettings {
  enabled: boolean;
  slug: string;
  title: string;
  projects: string[];
}

export function StatusPageForm({
  id,
  settings,
  projects,
  onSaved,
}: {
  id: string;
  settings: StatusSettings;
  projects: Project[];
  onSaved: () => void;
}) {
  return (
    <AsyncForm
      className="panel panel-pad workspace-settings status-settings"
      onSubmit={async (form) => {
        await request(
          `workspaces/${id}/status-page`,
          {
            enabled: form.has("enabled"),
            slug: text(form, "slug"),
            title: text(form, "title"),
            projects: form.getAll("projects"),
          },
          "PATCH",
        );
        onSaved();
      }}
    >
      <div>
        <h2>Status page</h2>
        <p>
          Publish a read-only page with uptime and incidents for selected
          projects.
        </p>
      </div>
      <label className="status-option">
        <input
          type="checkbox"
          name="enabled"
          defaultChecked={settings.enabled}
        />
        Publish status page
      </label>
      <div className="status-settings-fields">
        <Field
          label="Page title"
          name="title"
          defaultValue={settings.title}
          maxLength={120}
        />
        <Field
          label="Address slug"
          name="slug"
          defaultValue={settings.slug}
          placeholder="my-workspace"
          pattern="[A-Za-z0-9]([A-Za-z0-9\-]{0,61}[A-Za-z0-9])?"
          maxLength={63}
        />
      </div>
      <fieldset className="status-picker">
        <legend>Projects to publish</legend>
        {projects.length === 0 ? (
          <p>No projects in this workspace.</p>
        ) : (
          projects.map((project) => (
            <label className="status-option" key={project.id}>
              <input
                type="checkbox"
                name="projects"
                value={project.id}
                defaultChecked={settings.projects.includes(project.id)}
              />
              <span>{project.name}</span>
              <small>
                {project.uptime_monitoring ? "Monitoring on" : "Monitoring off"}
              </small>
            </label>
          ))
        )}
      </fieldset>
      <div className="row">
        <button className="primary">Save status page</button>
        {settings.enabled && (
          <a
            className="button"
            href={`/status/${settings.slug}`}
            target="_blank"
            rel="noreferrer"
          >
            Open status page
          </a>
        )}
      </div>
    </AsyncForm>
  );
}

function StatusPageSettings({ id }: { id: string }) {
  const ui = useUI();
  const feed = useLiveFeed<StatusSettings>({
    topic: "status_page",
    workspace_id: id,
  });
  const projects = useQuery(queries.projects(id));
  const liveError = useLiveSubscription({
    topic: "workspace",
    workspace_id: id,
  });
  if (feed.error || projects.error || liveError)
    return <ErrorMessage error={feed.error || projects.error || liveError} />;
  if (!feed.data || !projects.data) return <Loading />;
  return (
    <StatusPageForm
      key={id + JSON.stringify(feed.data)}
      id={id}
      settings={feed.data}
      projects={projects.data}
      onSaved={() => ui.notify("Status page saved")}
    />
  );
}

export function WorkspaceSettingsView() {
  const id = useParams().workspaceId!,
    { workspace } = useDashboard(),
    { can } = useSession(),
    ui = useUI(),
    client = useQueryClient();
  return (
    <>
      <Heading
        kicker="Workspace"
        title="Workspace settings"
        subtitle="A shared home for your projects."
      />
      <AsyncForm
        key={workspace?.id + (workspace?.name || "")}
        className="panel panel-pad workspace-settings"
        disabled={!can("workspace.manage", workspace)}
        onSubmit={async (form) => {
          await request(
            `workspaces/${id}`,
            { name: text(form, "name") },
            "PATCH",
          );
          await client.invalidateQueries(queries.workspaces());
          ui.notify("Workspace saved");
        }}
      >
        <Field
          label="Workspace name"
          name="name"
          defaultValue={workspace?.name}
          required
        />
        <button className="primary">Save changes</button>
      </AsyncForm>
      {can("workspace.manage", workspace) && <StatusPageSettings id={id} />}
    </>
  );
}
