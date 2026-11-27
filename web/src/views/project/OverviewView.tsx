import { Link } from "react-router-dom";
import { useProject } from "../../layouts/ProjectLayout";
import { ProjectHeading } from "../../components/ProjectHeading";
import { ResourceTransfer } from "../../components/ResourceTransfer";
import { ScopedActivityTabs } from "../../components/ScopedActivityTabs";
import { ProjectSettingsForm } from "../../components/ProjectSettingsForm";
import { Badge } from "../../components/ui";
import { Icon } from "../../components/Icon";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { request } from "../../services/api";

export function OverviewView() {
  const { project, releases, containers, routes, previews } = useProject();
  const { can } = useSession(),
    ui = useUI();
  const liveReleases = releases.filter((release) => !release.preview_id);
  const base = `/project/${project.id}`;
  return (
    <>
      <ProjectHeading
        title={project.name}
        actions={
          can("project.delete", project) && (
            <ResourceTransfer
              kind="projects"
              id={project.id}
              name={project.name}
              workspaceId={project.workspace_id}
            />
          )
        }
      />
      <ScopedActivityTabs scope="project" resource={project} />
      <div className="project-overview">
        <div className="overview-configuration">
          <ProjectSettingsForm />
        </div>
        <aside className="overview-aside" aria-label="Project at a glance">
          <section className="panel overview-panel">
            <div className="section-head">
              <h2>Recent deployments</h2>
              <Link to={`${base}/deployments`} className="subtle-link">
                View all <Icon name="next" />
              </Link>
            </div>
            {liveReleases.length ? (
              <div className="overview-releases">
                {liveReleases.slice(0, 3).map((release) => (
                  <Link
                    key={release.id}
                    to={`${base}/deployments?release=${encodeURIComponent(release.id)}`}
                    className="overview-release"
                  >
                    <div>
                      <span className="release-id">
                        <Icon name="branch" />
                        {release.commit?.slice(0, 7) || release.id.slice(0, 7)}
                      </span>
                      <time dateTime={release.created}>
                        {new Date(release.created).toLocaleString()}
                      </time>
                    </div>
                    <Badge
                      status={
                        project.sleeping &&
                        release.id === project.active_deployment
                          ? "sleeping"
                          : release.status
                      }
                    />
                  </Link>
                ))}
              </div>
            ) : (
              <div className="overview-empty">
                <span className="card-icon">
                  <Icon name="rocket" />
                </span>
                <strong>Ready for your first deployment</strong>
                <p>Your latest builds will appear here.</p>
                <Link className="button" to={`${base}/deployments`}>
                  Open deployments <Icon name="next" />
                </Link>
              </div>
            )}
          </section>
          {project.previews && (
            <section className="panel overview-panel" aria-label="Previews">
              <div className="section-head">
                <h2>Previews</h2>
                <span className="small muted">{previews.length}</span>
              </div>
              {previews.length ? (
                previews.map((preview) => {
                  const latest = releases.find(
                    (release) => release.preview_id === preview.id,
                  );
                  const status = preview.sleeping
                    ? "sleeping"
                    : [
                          "queued",
                          "building",
                          "testing",
                          "starting",
                          "ready",
                        ].includes(latest?.status || preview.status)
                      ? "building"
                      : latest?.status || preview.status;
                  return (
                    <div className="preview-row" key={preview.id}>
                      <div className="preview-row-heading">
                        <span className="release-id">
                          <Icon name="branch" />
                          {preview.branch}
                        </span>
                        <Badge status={status} />
                      </div>
                      <div className="preview-row-details">
                        <code className="small muted">
                          {preview.commit?.slice(0, 7) || "No commit yet"}
                        </code>
                        {can("deploy", project) && (
                          <button
                            className="subtle-link"
                            type="button"
                            aria-label={`Remove preview ${preview.branch}`}
                            onClick={() =>
                              ui.dialog({
                                title: `Remove preview ${preview.branch}?`,
                                description:
                                  "This stops its containers and removes its preview address. The live release is unaffected.",
                                submit: "Remove preview",
                                onSubmit: async () => {
                                  await request(
                                    `projects/${project.id}/previews/${preview.id}`,
                                    undefined,
                                    "DELETE",
                                  );
                                  ui.notify("Preview removed");
                                },
                              })
                            }
                          >
                            Remove
                          </button>
                        )}
                      </div>
                      {preview.addresses?.map((address) => (
                        <a
                          className="preview-address"
                          key={address}
                          href={address}
                          target="_blank"
                          rel="noreferrer"
                        >
                          {address}
                          <Icon name="next" />
                        </a>
                      ))}
                    </div>
                  );
                })
              ) : (
                <p className="small muted">
                  No previews yet. Push to another branch to deploy one.
                </p>
              )}
            </section>
          )}
          <section className="panel overview-panel">
            <div className="section-head">
              <h2>Project information</h2>
            </div>
            <dl className="overview-details">
              <div>
                <dt>Repository</dt>
                <dd>
                  <Link to={`${base}/source`}>
                    {project.git_url || "Connect a repository"}
                  </Link>
                </dd>
              </div>
              <div>
                <dt>Branch</dt>
                <dd>
                  <Icon name="branch" />
                  {project.branch || "main"}
                </dd>
              </div>
              <div>
                <dt>Containers</dt>
                <dd>
                  <Link to={`${base}/containers`}>
                    {containers.length} detected <Icon name="next" />
                  </Link>
                </dd>
              </div>
              <div>
                <dt>Domains</dt>
                <dd>
                  <Link to={`${base}/domains`}>
                    {routes.length} connected <Icon name="next" />
                  </Link>
                </dd>
              </div>
              <div>
                <dt>Deploy on push</dt>
                <dd>{project.auto_deploy ? "Enabled" : "Disabled"}</dd>
              </div>
              <div>
                <dt>Sleep when idle</dt>
                <dd>
                  {project.sleeping
                    ? "Sleeping"
                    : project.sleep_after
                      ? `After ${project.sleep_after} min`
                      : "Off"}
                </dd>
              </div>
            </dl>
          </section>
        </aside>
      </div>
    </>
  );
}
