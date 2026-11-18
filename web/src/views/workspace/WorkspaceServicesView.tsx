import { useState } from "react";
import { useQueryClient, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useSession } from "../../providers/SessionProvider";
import { useServices, externalEndpoint } from "../../services/services";
import { queries } from "../../services/queries";
import { Heading } from "../../components/Heading";
import { Badge, Empty, ErrorMessage, Loading } from "../../components/ui";
import { Icon } from "../../components/Icon";
import { EngineIcon } from "../../components/EngineIcon";
import { CopyValue } from "../../components/ServiceUI";
import { NewServiceModal } from "../../components/NewServiceModal";
import { ServiceActions } from "../../components/ServiceActions";
export function WorkspaceServicesView() {
  const id = useParams().workspaceId!,
    { workspace } = useDashboard(),
    { can } = useSession(),
    client = useQueryClient();
  const services = useServices(id),
    projects = useQuery(queries.projects(id)),
    catalog = useQuery(queries.catalog()),
    [open, setOpen] = useState(false);
  const allowed = can("services.manage", workspace),
    refresh = () => client.invalidateQueries();
  return (
    <div className="services-page">
      <Heading
        title="Services"
        kicker={workspace?.name}
        subtitle="Your application’s building blocks. Private, connected and ready to run."
        actions={
          allowed && (
            <button className="primary" onClick={() => setOpen(true)}>
              <Icon name="plus" />
              New service
            </button>
          )
        }
      />
      <ErrorMessage error={services.error || services.liveError} />
      {services.isPending ? (
        <Loading />
      ) : !services.data?.services.length ? (
        <Empty title="A home for your data">
          <p>
            Deploy a database, cache or queue and connect it to your projects.
          </p>
          {allowed && (
            <button onClick={() => setOpen(true)}>
              <Icon name="plus" />
              Explore the catalog
            </button>
          )}
        </Empty>
      ) : (
        <>
          <div className="service-list-meta">
            <span>{services.data.services.length} services</span>
            <span>
              <i className="live-dot" />
              Live updates
            </span>
          </div>
          <div className="services-table">
            <div className="service-table-head">
              <span>Service</span>
              <span>Status</span>
              <span>Endpoints</span>
              <span>Connected projects</span>
              <span />
            </div>
            {services.data.services.map((s) => (
              <div className="service-table-row" key={s.id}>
                <Link
                  className="service-identity"
                  to={`/workspace/${id}/services/${s.id}`}
                >
                  <EngineIcon engine={s.engine} />
                  <span>
                    <strong>{s.name}</strong>
                    <span>
                      {catalog.data?.find((e) => e.id === s.engine)?.name ||
                        s.engine}{" "}
                      <span className="service-version">{s.version}</span>
                    </span>
                  </span>
                </Link>
                <Badge status={s.status} />
                <div className="service-endpoints">
                  <CopyValue value={s.name} />
                  {s.external_port ? (
                    <CopyValue value={externalEndpoint(s)} />
                  ) : (
                    <span className="small muted">
                      <Icon name="lock" />
                      Internal only
                    </span>
                  )}
                </div>
                <div className="attachment-chips">
                  {s.attachments.length ? (
                    s.attachments.map((a) => (
                      <Link
                        key={a.project_id}
                        to={`/project/${a.project_id}/connections`}
                        className="attachment-chip"
                      >
                        <Icon name="plug" />
                        {projects.data?.find((p) => p.id === a.project_id)
                          ?.name || a.project_id}
                      </Link>
                    ))
                  ) : (
                    <span className="muted small">Not connected</span>
                  )}
                </div>
                <div className="service-row-actions">
                  {allowed && <ServiceActions service={s} onSaved={refresh} />}
                </div>
              </div>
            ))}
          </div>
          <div className="service-bottom-note">
            <Icon name="shield-check" />
            Services are private by default. Connect a project to inject its
            connection environment.
          </div>
        </>
      )}
      {open && (
        <NewServiceModal
          workspaceId={id}
          onClose={() => setOpen(false)}
          onSaved={refresh}
        />
      )}
    </div>
  );
}
