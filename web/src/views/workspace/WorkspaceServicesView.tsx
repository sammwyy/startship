import { useState } from "react";
import { useQueryClient, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useSession } from "../../providers/SessionProvider";
import { useServices, externalEndpoint } from "../../services/services";
import { queries } from "../../services/queries";
import { Heading } from "../../components/Heading";
import {
  CollectionLayout,
  useCollectionLayout,
} from "../../components/CollectionLayout";
import { Badge, Empty, ErrorMessage, Loading } from "../../components/ui";
import { Icon } from "../../components/Icon";
import { EngineIcon } from "../../components/EngineIcon";
import { CopyValue } from "../../components/ServiceUI";
import { NewServiceModal } from "../../components/NewServiceModal";
import { useLiveSubscription } from "../../providers/LiveProvider";
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
  useLiveSubscription({ topic: "services_catalog" });
  const [layout, setLayout] = useCollectionLayout("services");
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
              Add service
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
          <div className="collection-toolbar">
            <span>
              {services.data.services.length} services{" "}
              <span className="collection-live">
                <i className="live-dot" /> Live updates
              </span>
            </span>
            <CollectionLayout layout={layout} change={setLayout} />
          </div>
          {layout === "list" ? (
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
                    <EngineIcon
                      engine={
                        s.definition?.logo ??
                        catalog.data?.find((e) => e.id === s.engine)?.logo ??
                        s.engine
                      }
                    />
                    <span>
                      <strong>{s.name}</strong>
                      <span>
                        {s.definition?.name ||
                          catalog.data?.find((e) => e.id === s.engine)?.name ||
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
                    {allowed && (
                      <ServiceActions service={s} onSaved={refresh} />
                    )}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className={`collection collection-${layout}`}>
              {services.data.services.map((service) => {
                const engine =
                  service.definition ??
                  catalog.data?.find((item) => item.id === service.engine);
                return (
                  <Link
                    className="collection-item"
                    to={`/workspace/${id}/services/${service.id}`}
                    key={service.id}
                  >
                    <span className="collection-identity">
                      <EngineIcon engine={engine?.logo ?? service.engine} />
                      <span>
                        <strong>{service.name}</strong>
                        {layout === "cards" && (
                          <small>
                            {engine?.name ?? service.engine} · {service.version}
                          </small>
                        )}
                      </span>
                    </span>
                    {layout === "cards" && (
                      <>
                        <Badge status={service.status} />
                        <span className="collection-endpoint">
                          {service.external_port
                            ? externalEndpoint(service)
                            : "Internal only"}
                        </span>
                      </>
                    )}
                  </Link>
                );
              })}
            </div>
          )}
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
