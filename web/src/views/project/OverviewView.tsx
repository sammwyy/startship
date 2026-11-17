import { Link } from "react-router-dom";
import { useProject } from "../../layouts/ProjectLayout";
import { ProjectHeading } from "../../components/ProjectHeading";
import { ProjectSettingsForm } from "../../components/ProjectSettingsForm";
import { Badge } from "../../components/ui";
import { Icon } from "../../components/Icon";

export function OverviewView() {
  const { project, releases, containers, routes } = useProject();
  const base = `/project/${project.id}`;
  return (
    <>
      <ProjectHeading title={project.name} />
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
            {releases.length ? (
              <div className="overview-releases">
                {releases.slice(0, 3).map((release) => (
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
                    <Badge status={release.status} />
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
            </dl>
          </section>
        </aside>
      </div>
    </>
  );
}
