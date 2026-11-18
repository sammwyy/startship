import { useParams } from "react-router-dom";
import { VolumesPanel } from "../../components/VolumesPanel";
export function WorkspaceVolumesView() {
  return <VolumesPanel workspaceId={useParams().workspaceId!} />;
}
