import { useState } from "react";
import { Heading } from "../../components/Heading";
import {
  UptimeHistory,
  UptimeIncidents,
  UptimeLatency,
  UptimeState,
  uptimePercent,
} from "../../components/UptimeSummary";
import type { UptimeSummary } from "../../components/UptimeSummary";
import { ErrorMessage, Loading } from "../../components/ui";
import { useProject } from "../../layouts/ProjectLayout";
import { useLiveFeed } from "../../providers/LiveProvider";
import { useSession } from "../../providers/SessionProvider";
import { request } from "../../services/api";

export function UptimeView() {
  const { project } = useProject();
  const { can } = useSession();
  const { data, error, status } = useLiveFeed<UptimeSummary>({
    topic: "uptime",
    project_id: project.id,
  });
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<unknown>();
  const enabled = !!project.uptime_monitoring;
  async function toggle() {
    setSaving(true);
    setSaveError(undefined);
    try {
      await request(
        `projects/${project.id}`,
        { uptime_monitoring: !enabled },
        "PATCH",
      );
    } catch (err) {
      setSaveError(err);
    } finally {
      setSaving(false);
    }
  }
  return (
    <section className="uptime-view">
      <Heading
        kicker="Reliability"
        title="Uptime"
        project={project.name}
        subtitle="Readiness checks on the active release. Sleeping and static deployments are not counted."
        actions={
          <button
            type="button"
            className={enabled ? "" : "primary"}
            role="switch"
            aria-label="Uptime monitoring"
            aria-checked={enabled}
            disabled={saving || !can("project.settings", project)}
            onClick={toggle}
          >
            {enabled ? "Monitoring on" : "Enable monitoring"}
          </button>
        }
      />
      <ErrorMessage error={saveError || error} />
      {!data ? (
        !error && <Loading layout="stats" label="Loading uptime…" />
      ) : (
        <>
          <div className="uptime-kpis panel">
            {(
              [
                ["24 hours", data.uptime_24h],
                ["7 days", data.uptime_7d],
                ["30 days", data.uptime_30d],
              ] as const
            ).map(([label, value]) => (
              <div key={label}>
                <span>{label} uptime</span>
                <strong>{uptimePercent(value)}</strong>
              </div>
            ))}
            <div>
              <span>
                {status === "connected"
                  ? "Current state · live"
                  : "Last known state · disconnected"}
              </span>
              <UptimeState state={data.state} />
            </div>
          </div>
          <section className="panel uptime-panel">
            <h2>Check history</h2>
            <UptimeHistory history={data.history} />
          </section>
          <section className="panel uptime-panel">
            <h2>Probe response time</h2>
            <UptimeLatency checks={data.checks} />
          </section>
          <section className="panel uptime-panel">
            <h2>Recent incidents</h2>
            <UptimeIncidents incidents={data.incidents} />
          </section>
        </>
      )}
    </section>
  );
}
