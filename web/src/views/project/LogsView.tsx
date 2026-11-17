import { useNavigate, useParams } from "react-router-dom";
import { useProject } from "../../layouts/ProjectLayout";
import { ProjectHeading } from "../../components/ProjectHeading";
import { LogPanel, useLog } from "../../components/LogPanel";

export function LogsView() {
  const { project, containers, releases } = useProject(),
    { containerId } = useParams();
  const navigate = useNavigate();
  const active = containers.some((c) => c.id === containerId)
    ? containerId!
    : "";
  const log = useLog(releases[0]?.id || "", active);
  return (
    <>
      <ProjectHeading title="Logs" />
      <div className="section-head">
        <h2>Logs</h2>
        <label className="runtime-selector">
          Log source
          <select
            value={active}
            onChange={(e) =>
              navigate(
                `/project/${project.id}/logs${e.target.value ? `/${e.target.value}` : ""}`,
              )
            }
          >
            <option value="">Project build output</option>
            {containers.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name} · runtime
              </option>
            ))}
          </select>
        </label>
      </div>
      <LogPanel
        log={log.data?.log}
        status={log.data?.status}
        title={
          active
            ? `${containers.find((c) => c.id === active)?.name} · runtime`
            : "Project build output"
        }
        error={log.error}
      />
    </>
  );
}
