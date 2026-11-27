import { useNavigate, useParams } from "react-router-dom";
import { useProject } from "../../layouts/ProjectLayout";
import { ProjectHeading } from "../../components/ProjectHeading";
import { LogPanel, useRuntimeLog } from "../../components/LogPanel";
import { Empty } from "../../components/ui";

export function LogsView() {
  const { project, containers } = useProject(),
    { containerId } = useParams();
  const navigate = useNavigate();
  const active = containers.find((c) => c.id === containerId) ?? containers[0];
  const log = useRuntimeLog(active?.id);
  return (
    <div className="logs-view">
      <ProjectHeading title="Logs" />
      {active ? (
        <>
          <div className="logs-toolbar">
            <label className="runtime-selector">
              Container
              <select
                value={active.id}
                onChange={(e) =>
                  navigate(`/project/${project.id}/logs/${e.target.value}`)
                }
              >
                {containers.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <LogPanel
            log={log.data?.log}
            status={active.status}
            title={`${active.name} · runtime`}
            emptyMessage="No runtime output yet."
            error={log.error}
            loading={log.isFetching && !log.data}
          />
        </>
      ) : (
        <Empty title="No containers yet">
          <p>Deploy the project to see its runtime logs here.</p>
        </Empty>
      )}
    </div>
  );
}
