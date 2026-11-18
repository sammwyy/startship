import { Link, useParams } from "react-router-dom";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useSession } from "../../providers/SessionProvider";
import { useBackups, useServices } from "../../services/services";
import { Heading } from "../../components/Heading";
import { EngineIcon } from "../../components/EngineIcon";
import { Icon } from "../../components/Icon";
import { ErrorMessage } from "../../components/ui";
import {
  BackupStorage,
  BackupsPanel,
  scheduleLabel,
} from "../../components/BackupsPanel";
export function WorkspaceBackupsView() {
  const id = useParams().workspaceId!,
    { workspace } = useDashboard(),
    { can } = useSession(),
    services = useServices(id),
    backups = useBackups(id);
  const persistent = (services.data?.services || []).filter((s) => s.volume_id);
  return (
    <div className="services-page">
      <Heading
        title="Backups"
        kicker={workspace?.name}
        subtitle="Scheduled copies of your service volumes, ready to restore."
      />
      <ErrorMessage error={services.error || services.liveError} />
      <div className="service-detail-grid backups-layout">
        <div className="service-detail-main">
          <BackupsPanel
            workspaceId={id}
            services={services.data?.services || []}
            allowed={can("services.manage", workspace)}
          />
        </div>
        <aside>
          <section className="panel service-panel">
            <div className="service-section-head">
              <h2>Schedules</h2>
              <Icon name="history" />
            </div>
            {persistent.length ? (
              <ul className="backup-schedules">
                {persistent.map((s) => {
                  const last = backups.data?.backups.find(
                    (b) => b.service_id === s.id && b.status === "completed",
                  );
                  return (
                    <li key={s.id}>
                      <Link
                        to={`/workspace/${id}/services/${s.id}?tab=backups`}
                      >
                        <EngineIcon engine={s.engine} />
                        <span>
                          <strong>{s.name}</strong>
                          <span>
                            {scheduleLabel(s.backup)}
                            {last &&
                              ` · last ${new Date(last.created).toLocaleDateString(undefined, { month: "short", day: "numeric" })}`}
                          </span>
                        </span>
                        <span
                          className={`backup-dot ${s.backup?.enabled ? "on" : ""}`}
                          aria-hidden="true"
                        />
                      </Link>
                    </li>
                  );
                })}
              </ul>
            ) : (
              <p>
                Services with persistent storage appear here. In-memory services
                have nothing to back up.
              </p>
            )}
          </section>
          <BackupStorage workspaceId={id} />
        </aside>
      </div>
    </div>
  );
}
