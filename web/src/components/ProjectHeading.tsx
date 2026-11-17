import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { useProject } from "../layouts/ProjectLayout";
import { useUI } from "../providers/UIProvider";
import { request } from "../services/api";
import { projectSummary } from "../lib/project";
import { Heading } from "./Heading";

export function ProjectHeading({
  title,
  actions,
}: {
  title: string;
  actions?: ReactNode;
}) {
  const { project, containers, releases, routes, refresh } = useProject(),
    ui = useUI();
  return (
    <>
      <Heading
        title={title}
        project={title === project.name ? undefined : project.name}
        kicker={title === project.name ? "Project overview" : undefined}
        subtitle={projectSummary(containers, routes)}
        actions={actions}
      />
      {!releases.length && containers.some((c) => c.active_deployment) && (
        <p className="note">
          Existing containers remain online. Deploy the project to start its
          unified release history.
        </p>
      )}
      {project.config_conflict && (
        <div className="note">
          Legacy containers used different settings. Review{" "}
          <Link to="../environment">Environment</Link> and{" "}
          <Link to="../overview">General configuration</Link>, then confirm one
          shared configuration. Existing deployments stay online.
          <button
            onClick={() =>
              ui.run(async () => {
                await request(
                  `projects/${project.id}`,
                  { config_confirmed: true },
                  "PATCH",
                );
                await refresh();
              })
            }
          >
            Confirm shared configuration
          </button>
        </div>
      )}
      {project.source_conflict && (
        <p className="note">
          Choose one <Link to="../source">project source</Link> before
          deploying.
        </p>
      )}
    </>
  );
}
