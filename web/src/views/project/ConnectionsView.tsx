import { useQueryClient } from "@tanstack/react-query";
import { useProject } from "../../layouts/ProjectLayout";
import { useSession } from "../../providers/SessionProvider";
import { useResourceDeployNeeded } from "../../services/services";
import { ProjectHeading } from "../../components/ProjectHeading";
import { RedeployNote } from "../../components/ServiceUI";
import { ConnectionsPanel } from "../../components/ConnectionsPanel";
export function ConnectionsView() {
  const { project } = useProject(),
    { can } = useSession(),
    client = useQueryClient(),
    changed = useResourceDeployNeeded(project);
  return (
    <div className="services-page">
      <ProjectHeading title="Connections" />
      <p className="resource-intro">
        Private links to projects, apps and services in this workspace. Each
        connection gets its own network, an internal hostname and environment
        variables. Use workspace networks to group several resources.
      </p>
      {changed && <RedeployNote />}
      <ConnectionsPanel
        workspaceId={project.workspace_id}
        endpoint={{ kind: "project", id: project.id }}
        canManage={can("project.settings", project)}
        onChanged={() => void client.invalidateQueries()}
      />
    </div>
  );
}
