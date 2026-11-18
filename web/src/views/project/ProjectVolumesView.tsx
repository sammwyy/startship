import { useProject } from "../../layouts/ProjectLayout";
import { VolumesPanel } from "../../components/VolumesPanel";
export function ProjectVolumesView() {
  const { project } = useProject();
  return <VolumesPanel workspaceId={project.workspace_id} project={project} />;
}
