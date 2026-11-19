import { useState, type ReactNode } from "react";
import { useProject } from "../layouts/ProjectLayout";
import { projectSummary } from "../lib/project";
import { Heading } from "./Heading";
import { Badge } from "./ui";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { request } from "../services/api";

export function ProjectHeading({
  title,
  actions,
}: {
  title: string;
  actions?: ReactNode;
}) {
  const { project, containers, routes } = useProject();
  const { can } = useSession();
  const ui = useUI();
  const [waking, setWaking] = useState(false);
  return (
    <Heading
      title={title}
      project={title === project.name ? undefined : project.name}
      kicker={title === project.name ? "Project overview" : undefined}
      subtitle={projectSummary(containers, routes, project.sleeping)}
      actions={
        project.sleeping || actions ? (
          <>
            {project.sleeping && (
              <>
                <Badge status="sleeping" />
                {can("deploy", project) && (
                  <button
                    disabled={waking}
                    onClick={() =>
                      ui.run(async () => {
                        setWaking(true);
                        try {
                          await request(
                            `projects/${project.id}/wake`,
                            {},
                            "POST",
                          );
                          ui.notify("Project awake");
                        } finally {
                          setWaking(false);
                        }
                      })
                    }
                  >
                    {waking ? "Waking…" : "Wake now"}
                  </button>
                )}
              </>
            )}
            {actions}
          </>
        ) : undefined
      }
    />
  );
}
