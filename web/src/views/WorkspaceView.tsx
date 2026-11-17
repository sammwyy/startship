import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQueryClient, useSuspenseQueries } from "@tanstack/react-query";
import { useDashboard } from "../layouts/DashboardLayout";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { queries } from "../services/queries";
import { request } from "../services/api";
import type { Project } from "../services/types";
import { sourceData, sortReleases, text } from "../lib/project";
import { SourceFields } from "../components/SourceFields";
import { Heading } from "../components/Heading";
import { Badge, Empty } from "../components/ui";
import { Icon } from "../components/Icon";
import {
  useLiveProjects,
  useLiveSubscription,
} from "../providers/LiveProvider";

export function WorkspaceView() {
  const id = useParams().workspaceId!,
    { workspace } = useDashboard(),
    { can } = useSession(),
    ui = useUI(),
    navigate = useNavigate(),
    client = useQueryClient();
  const [{ data: projects }, { data: allContainers }, { data: allReleases }] =
    useSuspenseQueries({
      queries: [queries.projects(id), queries.containers(), queries.releases()],
    });
  const [search, setSearch] = useState("");
  useLiveSubscription({ topic: "workspace", workspace_id: id });
  useLiveProjects(projects.map((project) => project.id));
  const containers = allContainers.filter((c) =>
    projects.some((p) => p.id === c.project_id),
  );
  const releases = sortReleases(
    allReleases.filter((r) => projects.some((p) => p.id === r.project_id)),
  );
  const visible = projects.filter((p) =>
    p.name.toLowerCase().includes(search.trim().toLowerCase()),
  );
  const create = () =>
    ui.dialog({
      title: "Create project",
      description:
        "One repository, one deployment. Startship detects the containers your project needs.",
      submit: "Create project",
      fields: (
        <>
          <label>
            Project name
            <input name="name" required placeholder="My Website" />
          </label>
          <SourceFields />
        </>
      ),
      onSubmit: async (form) => {
        const project = await request<Project>("projects", {
          name: text(form, "name"),
          workspace_id: id,
          ...sourceData(form),
        });
        await client.invalidateQueries(queries.projects(id));
        navigate(`/project/${project.id}`);
        ui.notify("Project created");
      },
    });
  return (
    <>
      <Heading
        title="Projects"
        kicker="Workspace overview"
        subtitle="One source and deployment per project."
        actions={
          can("projects.create", workspace) && (
            <button className="primary" onClick={create}>
              <Icon name="plus" />
              New project
            </button>
          )
        }
      />
      <div className="stats">
        {[
          ["Total projects", projects.length, "grid"],
          ["Containers", containers.length, "box"],
          [
            "Running containers",
            containers.filter((c) => c.status === "running").length,
            "activity",
          ],
        ].map(([label, count, icon]) => (
          <div
            className={`stat ${icon === "activity" ? "stat-positive" : ""}`}
            key={label}
          >
            <div>
              <span>{label}</span>
              <strong>{count}</strong>
            </div>
            <Icon name={String(icon)} />
          </div>
        ))}
      </div>
      <div className="workspace-layout">
        <section>
          <div className="project-search">
            <label className="search-field">
              <Icon name="search" />
              <input
                aria-label="Search projects"
                type="search"
                placeholder="Find a project"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </label>
          </div>
          {projects.length ? (
            <div className="panel project-directory">
              {visible.map((p) => {
                const owned = containers.filter(
                    (c) => c.project_id === p.id && c.active_deployment,
                  ),
                  latest = releases.find((r) => r.project_id === p.id);
                return (
                  <Link
                    className="project-row"
                    key={p.id}
                    to={`/project/${p.id}`}
                  >
                    <span className="card-icon">
                      <Icon name="grid" />
                    </span>
                    <span className="project-row-copy">
                      <h3>{p.name}</h3>
                      <span>
                        {owned.length} containers ·{" "}
                        {p.git_url
                          ? p.branch || "main"
                          : "Connect a repository"}
                      </span>
                    </span>
                    <Badge
                      status={
                        latest?.status ||
                        (owned.some((c) => c.status === "running")
                          ? "running"
                          : "")
                      }
                    />
                    <Icon name="arrow" />
                  </Link>
                );
              })}
              {!visible.length && (
                <p className="panel-pad small muted">
                  No projects match your search.
                </p>
              )}
            </div>
          ) : (
            <Empty title="No projects yet">
              Connect a repository to ship your first project.
            </Empty>
          )}
        </section>
        <aside className="panel activity-panel">
          <div className="section-head">
            <h2>Deployment activity</h2>
            <Icon name="activity" />
          </div>
          <div className="activity-total">
            <strong>{releases.length}</strong>
            <span>project releases</span>
          </div>
          {releases.length ? (
            <div
              className="release-pulse"
              aria-label="Recent deployment statuses"
            >
              {releases
                .slice(0, 32)
                .reverse()
                .map((r) => (
                  <span key={r.id} className={r.status} title={r.status} />
                ))}
            </div>
          ) : (
            <p className="activity-empty">
              Your first release starts with a deploy.
            </p>
          )}
          {!!releases.length && (
            <div className="release-list">
              <h2>Recent releases</h2>
              {releases.slice(0, 4).map((r) => (
                <Link
                  key={r.id}
                  className="release-item"
                  to={`/project/${r.project_id}/deployments?release=${encodeURIComponent(r.id)}`}
                >
                  <div>
                    <strong>
                      {projects.find((p) => p.id === r.project_id)?.name ||
                        "Project"}
                    </strong>
                    <small>{r.commit?.slice(0, 7) || r.id.slice(0, 7)}</small>
                  </div>
                  <Badge status={r.status} />
                </Link>
              ))}
            </div>
          )}
        </aside>
      </div>
    </>
  );
}
