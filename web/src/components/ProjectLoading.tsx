import { Heading } from "./Heading";
import { Loading, Skeleton } from "./Skeleton";
import { scopedActivityTitle } from "./ScopedActivityTabs";
import { projectSections } from "../lib/project";

function HeadingLine({ width }: { width: string }) {
  return (
    <span className="project-skeleton-line" aria-hidden="true">
      <Skeleton width={width} height="0.55em" />
    </span>
  );
}

function OverviewPanels() {
  return (
    <div role="status" aria-label="Loading project overview">
      <div className="project-overview" aria-hidden="true">
        <div className="overview-configuration">
          <div className="panel panel-pad project-skeleton-form">
            <Skeleton width="9rem" />
            <Skeleton width="min(16rem, 100%)" />
            <div className="split">
              {[0, 1].map((field) => (
                <div key={field} className="project-skeleton-field">
                  <Skeleton width="5rem" />
                  <Skeleton height="2.5rem" />
                </div>
              ))}
            </div>
            {[0, 1, 2].map((option) => (
              <div key={option} className="project-skeleton-option">
                <Skeleton width="0.875rem" height="0.875rem" />
                <Skeleton width="8rem" />
              </div>
            ))}
            <Skeleton width="90%" />
            <Skeleton width="70%" />
            <div className="configuration-actions">
              <Skeleton width="45%" />
              <Skeleton width="6rem" height="2.25rem" />
            </div>
          </div>
        </div>
        <div className="overview-aside">
          <div className="panel overview-panel">
            <div className="section-head">
              <Skeleton width="8rem" />
              <Skeleton width="3rem" />
            </div>
            {[0, 1, 2].map((deployment) => (
              <div className="overview-release" key={deployment}>
                <div className="project-skeleton-field">
                  <Skeleton width="4.5rem" />
                  <Skeleton width="7rem" />
                </div>
                <Skeleton width="3rem" />
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function LogSkeleton({ label }: { label: string }) {
  return (
    <div
      className="panel log-panel project-log-panel"
      role="status"
      aria-label={label}
    >
      <div className="log-head" aria-hidden="true">
        <Skeleton width="6rem" />
        <Skeleton width="7rem" height="2rem" />
      </div>
      <div className="log project-skeleton-field" aria-hidden="true">
        <Skeleton width="min(18rem, 80%)" />
        <Skeleton width="min(12rem, 60%)" />
        <Skeleton width="min(20rem, 85%)" />
      </div>
      <div className="log-footer" aria-hidden="true">
        <Skeleton width="4rem" />
        <Skeleton width="7rem" />
      </div>
    </div>
  );
}

export function ProjectLoading({
  section,
  projectName,
  showActivityTabs,
  canEditSettings,
}: {
  section: string;
  projectName?: string;
  showActivityTabs: boolean;
  canEditSettings: boolean;
}) {
  const overview = section === "overview";
  const activity = section === "activity";
  const deployments = section === "deployments";
  const title = activity
    ? scopedActivityTitle("project")
    : projectSections.find(([path]) => path === section)?.[1];
  const heading = (
    <Heading
      title={
        overview
          ? (projectName ?? <HeadingLine width="min(12rem, 55vw)" />)
          : (title ?? <HeadingLine width="8rem" />)
      }
      kicker={
        overview
          ? "Project overview"
          : activity
            ? "Project"
            : section === "uptime"
              ? "Reliability"
              : undefined
      }
      project={
        overview || activity
          ? undefined
          : (projectName ?? <HeadingLine width="6rem" />)
      }
      subtitle={<HeadingLine width="min(17rem, 60vw)" />}
      actions={
        deployments && (
          <div className="deployment-actions" aria-hidden="true">
            <Skeleton width="7rem" />
            <Skeleton width="7rem" height="2.25rem" />
            <Skeleton width="4.5rem" height="2.25rem" />
          </div>
        )
      }
    />
  );
  if (section === "logs")
    return (
      <div className="logs-view">
        {heading}
        <div className="logs-toolbar" aria-hidden="true">
          <Skeleton width="10rem" height="var(--logs-control-height)" />
        </div>
        <LogSkeleton label="Loading logs" />
      </div>
    );
  return (
    <>
      {heading}
      {(overview || activity) && showActivityTabs && (
        <div
          className="settings-navigation project-skeleton-tabs"
          aria-hidden="true"
        >
          <Skeleton width="4.25rem" />
          <Skeleton width="5rem" />
        </div>
      )}
      {overview ? (
        <>
          {!canEditSettings && (
            <p className="note locked-note" aria-hidden="true">
              <HeadingLine width="min(20rem, 60vw)" />
            </p>
          )}
          <OverviewPanels />
        </>
      ) : deployments ? (
        <div className="project-deployments">
          <div className="panel release-history">
            <Loading label="Loading deployment history" />
          </div>
          <LogSkeleton label="Loading deployment logs" />
        </div>
      ) : (
        <Loading layout="table" label="Loading project data" />
      )}
    </>
  );
}
