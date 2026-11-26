import { useEffect, useState } from "react";
import { Icon } from "../components/Icon";
import { ErrorMessage, Loading, ThemeButton } from "../components/ui";
import {
  UptimeHistory,
  UptimeIncidents,
  UptimeState,
  uptimePercent,
} from "../components/UptimeSummary";
import type { UptimeBucket, UptimeIncident } from "../components/UptimeSummary";

interface PublicProjectStatus {
  name: string;
  state: string;
  uptime: number | null;
  history: UptimeBucket[];
  incidents: UptimeIncident[];
}

interface PublicStatusPage {
  title: string;
  projects: PublicProjectStatus[];
}

export function PublicStatusView({ slug }: { slug: string }) {
  const [page, setPage] = useState<PublicStatusPage>();
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    const controller = new AbortController();
    fetch(`/api/status/${encodeURIComponent(slug)}`, {
      credentials: "omit",
      signal: controller.signal,
    })
      .then((response) => {
        if (!response.ok)
          throw new Error(
            response.status === 404
              ? "This status page is not published."
              : "Status is unavailable.",
          );
        return response.json() as Promise<PublicStatusPage>;
      })
      .then((data) => {
        document.title = `${data.title} · Status`;
        setPage(data);
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setError(err);
      });
    return () => controller.abort();
  }, [slug]);

  const states = page?.projects.map(({ state }) => state) ?? [];
  const [state, banner] = states.includes("down")
    ? ["down", "Service disruption detected"]
    : states.includes("degraded")
      ? ["degraded", "Some health checks are failing"]
      : states.length && states.every((state) => state === "operational")
        ? ["operational", "All systems operational"]
        : ["pending", "Some services have no current checks"];
  return (
    <main className="public-status">
      <div className="status-shell">
        <header className="status-header">
          <span className="brand">
            <span className="mark">
              <Icon name="activity" />
            </span>{" "}
            Service status
          </span>
          <ThemeButton />
        </header>
        <div className="status-title">
          <div className="eyebrow">Public status page</div>
          <h1>{page?.title ?? "Service status"}</h1>
          <p>Availability and incident history for published services.</p>
        </div>
        {error ? (
          <ErrorMessage error={error} />
        ) : !page ? (
          <Loading />
        ) : (
          <>
            <div className={`status-banner uptime-${state}`} role="status">
              <Icon name="activity" />
              {banner}
            </div>
            {page.projects.length === 0 && <p>No projects are published.</p>}
            {page.projects.map((project, index) => (
              <article
                className="panel uptime-panel status-project"
                key={index}
              >
                <header>
                  <h2>{project.name}</h2>
                  <UptimeState state={project.state} />
                </header>
                <div className="status-project-metric">
                  <strong>{uptimePercent(project.uptime)}</strong>
                  <span>30-day uptime</span>
                </div>
                <UptimeHistory history={project.history} />
                <h3>Recent incidents</h3>
                <UptimeIncidents incidents={project.incidents} />
              </article>
            ))}
            <footer>Status as of page load · Refresh to update</footer>
          </>
        )}
      </div>
    </main>
  );
}
