import { useState } from "react";
import { useProject } from "../../layouts/ProjectLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { request } from "../../services/api";
import { busyRelease } from "../../lib/project";
import { ProjectHeading } from "../../components/ProjectHeading";
import { LogPanel, useLog } from "../../components/LogPanel";
import { Badge } from "../../components/ui";
import { Icon } from "../../components/Icon";

export function DeploymentsView() {
  const { project, releases, refresh } = useProject(),
    { can } = useSession(),
    ui = useUI();
  const [chosen, setChosen] = useState<string | null>(null),
    [noCache, setNoCache] = useState(false),
    [pending, setPending] = useState(false);
  const selected = chosen ?? releases[0]?.id ?? "";
  const log = useLog(selected);
  const busy = busyRelease(releases[0]) || pending;
  async function deploy(redeploy = false) {
    setPending(true);
    try {
      await request(
        `projects/${project.id}/${redeploy ? "redeploy" : "deploy"}`,
        redeploy ? { deployment_id: selected } : { no_cache: noCache },
      );
      setChosen(null);
      await refresh();
      ui.notify("Project deployment queued");
    } finally {
      setPending(false);
    }
  }
  return (
    <>
      <ProjectHeading
        title="Deployments"
        actions={
          <button
            className="primary"
            disabled={
              busy ||
              !project.git_url ||
              !!project.config_conflict ||
              !!project.source_conflict ||
              !can("deploy", project)
            }
            onClick={() => ui.run(() => deploy())}
          >
            <Icon name="plus" />
            Deploy
          </button>
        }
      />
      <div className="project-deployments">
        <section className="release-history">
          <div className="section-head">
            <h2>Project releases</h2>
            <span className="count">{releases.length}</span>
          </div>
          <div className="panel release-directory">
            {releases.map((r) => (
              <button
                key={r.id}
                className={`release-entry select-deploy ${selected === r.id ? "selected" : ""}`}
                aria-pressed={selected === r.id}
                onClick={() => setChosen(r.id)}
              >
                <span className="release-main">
                  <span className="release-id">
                    <Icon name="branch" />
                    {r.commit?.slice(0, 7) || r.id.slice(0, 7)}
                  </span>
                  <Badge status={r.status} />
                </span>
                <span className="release-meta">
                  <time>{new Date(r.created).toLocaleString()}</time>
                  <span>
                    {r.container_releases?.length || 0} containers
                    {r.redeploy_of ? " · redeployed" : ""}
                  </span>
                </span>
              </button>
            ))}
            {!releases.length && (
              <p className="panel-pad small muted">
                No project deployments yet.
              </p>
            )}
          </div>
          <div className="release-toolbar">
            <label className="inline-check">
              <input
                type="checkbox"
                checked={noCache}
                onChange={(e) => setNoCache(e.target.checked)}
              />
              Build without cache
            </label>
            <button
              disabled={
                busy || !log.data?.redeployable || !can("deploy", project)
              }
              onClick={() => ui.run(() => deploy(true))}
            >
              Redeploy selected
            </button>
          </div>
        </section>
        <LogPanel
          log={log.data?.log}
          status={log.data?.status}
          error={log.error}
        />
      </div>
    </>
  );
}
