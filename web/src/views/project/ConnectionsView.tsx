import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useProject } from "../../layouts/ProjectLayout";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { queries } from "../../services/queries";
import { useServices } from "../../services/services";
import { remove } from "../../services/api";
import { ProjectHeading } from "../../components/ProjectHeading";
import { Icon } from "../../components/Icon";
import { EngineIcon } from "../../components/EngineIcon";
import { Badge, Empty, ErrorMessage, Loading } from "../../components/ui";
import { EnvPreview, RedeployNote } from "../../components/ServiceUI";
import { ConnectServiceModal } from "../../components/ConnectServiceModal";
export function ConnectionsView() {
  const { project } = useProject(),
    { workspace } = useDashboard(),
    { can } = useSession(),
    ui = useUI(),
    client = useQueryClient();
  const connections = useQuery(queries.projectServices(project.id)),
    services = useServices(project.workspace_id),
    catalog = useQuery(queries.catalog()),
    projects = useQuery(queries.projects(project.workspace_id));
  const [open, setOpen] = useState(false),
    [changed, setChanged] = useState(false),
    allowed =
      can("services.manage", workspace) && can("project.settings", project);
  async function refresh() {
    setChanged(true);
    await client.invalidateQueries();
  }
  return (
    <div className="services-page">
      <ProjectHeading
        title="Connections"
        actions={
          allowed && (
            <button className="primary" onClick={() => setOpen(true)}>
              <Icon name="plug" />
              Connect service
            </button>
          )
        }
      />
      <p className="resource-intro">
        Private connections to your workspace services, delivered as environment
        variables.
      </p>
      {changed && <RedeployNote />}
      <ErrorMessage
        error={
          connections.error ||
          services.error ||
          services.liveError ||
          catalog.error
        }
      />
      {connections.isPending ? (
        <Loading />
      ) : !connections.data?.attached.length ? (
        <Empty title="Connect your building blocks">
          <p>
            Attach a workspace database, cache or queue. No public ports needed.
          </p>
          {allowed && (
            <button onClick={() => setOpen(true)}>Connect service</button>
          )}
        </Empty>
      ) : (
        <div className="connection-cards">
          {connections.data.attached.map(({ service, alias, env }) => (
            <section className="panel connection-card" key={service.id}>
              <div className="connection-card-head">
                <div className="service-identity">
                  <EngineIcon engine={service.engine} />
                  <span>
                    <strong>{service.name}</strong>
                    <span>
                      {service.engine} · {service.version}{" "}
                      <span className="attachment-chip">{alias}</span>
                    </span>
                  </span>
                </div>
                <div className="row">
                  <Badge status={service.status} />
                  {allowed && (
                    <button
                      className="icon-button danger"
                      aria-label={`Detach ${service.name}`}
                      onClick={() =>
                        ui.dialog({
                          title: `Disconnect ${service.name}?`,
                          description:
                            "The service and its data will stay in the workspace. Redeploy this project to apply the change.",
                          submit: "Disconnect",
                          onSubmit: async () => {
                            await remove(
                              `services/${service.id}/attachments/${project.id}`,
                            );
                            await refresh();
                          },
                        })
                      }
                    >
                      <Icon name="close" />
                    </button>
                  )}
                </div>
              </div>
              <EnvPreview env={env} canReveal={allowed} />
            </section>
          ))}
        </div>
      )}
      {open && (
        <ConnectServiceModal
          workspaceId={project.workspace_id}
          engines={catalog.data || []}
          services={
            services.data?.services || connections.data?.available || []
          }
          projects={projects.data || []}
          project={project}
          onClose={() => setOpen(false)}
          onSaved={refresh}
        />
      )}
    </div>
  );
}
