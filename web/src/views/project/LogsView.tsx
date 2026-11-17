import { useState } from "react";
import { useParams } from "react-router-dom";
import { useProject } from "../../layouts/ProjectLayout";
import { ProjectHeading } from "../../components/ProjectHeading";
import { LogPanel, useLog } from "../../components/LogPanel";

export function LogsView() {
  const { containers, releases } = useProject(),
    { containerId } = useParams();
  const [source, setSource] = useState(containerId || "");
  const active = containers.some((c) => c.id === source) ? source : "";
  const log = useLog(releases[0]?.id || "", active);
  return (
    <>
      <ProjectHeading title="Logs" />
      <div className="section-head">
        <h2>Logs</h2>
        <label className="runtime-selector">
          Log source
          <select value={active} onChange={(e) => setSource(e.target.value)}>
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
