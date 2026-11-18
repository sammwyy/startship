import type { ReactNode } from "react";
import { useProject } from "../layouts/ProjectLayout";
import { projectSummary } from "../lib/project";
import { Heading } from "./Heading";

export function ProjectHeading({
  title,
  actions,
}: {
  title: string;
  actions?: ReactNode;
}) {
  const { project, containers, routes } = useProject();
  return (
    <Heading
      title={title}
      project={title === project.name ? undefined : project.name}
      kicker={title === project.name ? "Project overview" : undefined}
      subtitle={projectSummary(containers, routes)}
      actions={actions}
    />
  );
}
