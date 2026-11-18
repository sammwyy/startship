import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { queries } from "../../services/queries";
import { request, remove } from "../../services/api";
import {
  connectionEnv,
  externalEndpoint,
  useServices,
  validServiceName,
} from "../../services/services";
import { Heading } from "../../components/Heading";
import { Icon } from "../../components/Icon";
import { EngineIcon } from "../../components/EngineIcon";
import {
  Badge,
  Empty,
  ErrorMessage,
  Field,
  Loading,
} from "../../components/ui";
import {
  CopyValue,
  EnvPreview,
  RedeployNote,
} from "../../components/ServiceUI";
import { ServiceConfig } from "../../components/ServiceConfig";
import { ServiceActions } from "../../components/ServiceActions";
import { ConnectServiceModal } from "../../components/ConnectServiceModal";
import {
  BackupStorage,
  BackupsPanel,
  ScheduleEditor,
} from "../../components/BackupsPanel";
export function ServiceDetailView() {
  const { workspaceId: id, serviceId } = useParams(),
    { workspace } = useDashboard(),
    { can } = useSession(),
    ui = useUI(),
    client = useQueryClient(),
    navigate = useNavigate();
  const list = useServices(id!),
    catalog = useQuery(queries.catalog()),
    projects = useQuery(queries.projects(id!));
  const [tab, setTab] = useState(
      useSearchParams()[0].get("tab") || "connection",
    ),
    [alias, setAlias] = useState(""),
    [reveal, setReveal] = useState(false),
    [open, setOpen] = useState(false),
    [changed, setChanged] = useState(false);
  const s = list.data?.services.find((s) => s.id === serviceId),
    engine = catalog.data?.find((e) => e.id === s?.engine),
    allowed = can("services.manage", workspace);
  const logs = useQuery({
    queryKey: [`services/${serviceId}/logs?tail=200`],
    queryFn: () =>
      request<{ log: string }>(`services/${serviceId}/logs?tail=200`),
    enabled: tab === "logs" && !!s,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  async function refresh() {
    await client.invalidateQueries();
  }
  if (list.isPending || catalog.isPending) return <Loading />;
  if (!s || !engine)
    return (
      <>
        <ErrorMessage error={list.error || catalog.error} />
        <Empty title="Service unavailable">
          <p>It may have been deleted, or you may no longer have access.</p>
          <Link className="button" to={`/workspace/${id}/services`}>
            Back to services
          </Link>
        </Empty>
      </>
    );
  const env = connectionEnv(s, engine, alias || s.name),
    editableProjects = (projects.data || []).filter((p) =>
      can("project.settings", p),
    );
  return (
    <div className="services-page">
      <Link className="service-back" to={`/workspace/${id}/services`}>
        <Icon name="back" />
        All services
      </Link>
      <Heading
        title={s.name}
        kicker={`${engine.name} / ${s.version}`}
        subtitle="Workspace infrastructure"
        actions={
          <>
            <Badge status={s.status} />
            {allowed && (
              <ServiceActions
                service={s}
                onSaved={async () => {
                  await refresh();
                  if (
                    !client
                      .getQueryData<{ services: { id: string }[] }>(
                        queries.services(id!).queryKey,
                      )
                      ?.services.some((item) => item.id === s.id)
                  )
                    navigate(`/workspace/${id}/services`);
                }}
              />
            )}
          </>
        }
      />
      <ErrorMessage error={list.liveError || s.last_error} />
      {changed && <RedeployNote />}
      <div className="service-detail-banner">
        <EngineIcon engine={s.engine} />
        <div>
          <strong>{engine.name}</strong>
          <span>
            {engine.image}:{s.version}
          </span>
        </div>
        <span className="service-storage-tag">
          <Icon name={s.volume_id ? "hard-drive" : "box"} />
          {s.volume_id ? "Persistent storage" : "In-memory only"}
        </span>
      </div>
      <nav className="service-tabs" aria-label="Service sections">
        {[
          "connection",
          "logs",
          ...(s.volume_id ? ["backups"] : []),
          ...(allowed ? ["settings"] : []),
        ].map((t) => (
          <button
            key={t}
            className={tab === t ? "active" : ""}
            onClick={() => setTab(t)}
          >
            {t[0].toUpperCase() + t.slice(1)}
          </button>
        ))}
      </nav>
      {tab === "connection" && (
        <div className="service-detail-grid">
          <div className="service-detail-main">
            <section className="panel service-panel">
              <div className="service-section-head">
                <h2>Connection details</h2>
                <span className="small muted">
                  <Icon name="lock" />
                  Private network
                </span>
              </div>
              <p>
                Reach this service from any attached project at {s.name}.local,
                or use the alias each project chose.
              </p>
              <div className="connection-values">
                <div>
                  <span>Hostname</span>
                  <CopyValue value={`${s.name}.local`} />
                </div>
                <div>
                  <span>Port</span>
                  <CopyValue value={String(engine.port)} />
                </div>
                {s.config.user && (
                  <div>
                    <span>User</span>
                    <CopyValue value={s.config.user} />
                  </div>
                )}
                {s.config.database && (
                  <div>
                    <span>Database</span>
                    <CopyValue value={s.config.database} />
                  </div>
                )}
                {allowed && s.config.password && (
                  <div>
                    <span>Password</span>
                    <span className="row">
                      <CopyValue
                        value={s.config.password}
                        label={reveal ? s.config.password : "••••••••"}
                      />
                      <button
                        className="icon-button"
                        aria-label={
                          reveal ? "Hide password" : "Reveal password"
                        }
                        onClick={() => setReveal(!reveal)}
                      >
                        <Icon name="eye" />
                      </button>
                    </span>
                  </div>
                )}
              </div>
              <div className="service-alias-input">
                <Field
                  label="Preview an attachment alias"
                  name="alias"
                  value={alias}
                  placeholder={s.name}
                  onChange={(e) => setAlias(e.target.value)}
                  aria-invalid={!!alias && !validServiceName(alias)}
                />
                <span className="small muted">
                  Alias values below are injected when this service is attached.
                </span>
              </div>
              <EnvPreview env={env} canReveal={allowed} />
              <div className="connection-url">
                <span className="small muted">Connection URL</span>
                <CopyValue
                  value={
                    Object.entries(env).find(([key]) =>
                      key.endsWith("_URL"),
                    )![1]
                  }
                  label={
                    s.config.password
                      ? "Copy connection URL"
                      : Object.entries(env).find(([key]) =>
                          key.endsWith("_URL"),
                        )![1]
                  }
                />
              </div>
            </section>
          </div>
          <aside>
            <section className="panel service-panel">
              <div className="service-section-head">
                <h2>External access</h2>
                <Icon name="globe" />
              </div>
              {s.external_port ? (
                <>
                  <p>
                    Published on the server’s host port. Ensure your firewall
                    permits access.
                  </p>
                  <CopyValue value={externalEndpoint(s)} />
                </>
              ) : (
                <p>
                  Internal only. Public access is disabled for this service.
                </p>
              )}
            </section>
            <section className="panel service-panel">
              <div className="service-section-head">
                <h2>Attached projects</h2>
                {allowed && (
                  <button
                    className="icon-button"
                    aria-label="Attach project"
                    onClick={() => setOpen(true)}
                  >
                    <Icon name="plus" />
                  </button>
                )}
              </div>
              {s.attachments.length ? (
                s.attachments.map((a) => (
                  <div className="attached-project-row" key={a.project_id}>
                    <Icon name="plug" />
                    <Link to={`/project/${a.project_id}/connections`}>
                      <strong>
                        {projects.data?.find((p) => p.id === a.project_id)
                          ?.name || a.project_id}
                      </strong>
                      <span>{a.alias}.local</span>
                    </Link>
                    {allowed &&
                      editableProjects.some((p) => p.id === a.project_id) && (
                        <button
                          className="icon-button"
                          aria-label={`Detach ${a.alias}`}
                          onClick={() =>
                            ui.dialog({
                              title: `Disconnect ${a.alias}?`,
                              description:
                                "Redeploy the project to apply this change. Service data is kept.",
                              submit: "Disconnect",
                              onSubmit: async () => {
                                await remove(
                                  `services/${s.id}/attachments/${a.project_id}`,
                                );
                                setChanged(true);
                                await refresh();
                              },
                            })
                          }
                        >
                          <Icon name="close" />
                        </button>
                      )}
                  </div>
                ))
              ) : (
                <p>
                  No projects connected yet. Attach a project to share a private
                  connection.
                </p>
              )}
            </section>
          </aside>
        </div>
      )}
      {tab === "logs" && (
        <section className="panel service-panel">
          <div className="service-section-head">
            <h2>
              Container logs <span className="small muted">Last 200 lines</span>
            </h2>
            <button
              disabled={logs.isFetching}
              onClick={() => void logs.refetch()}
            >
              <Icon name="history" />
              Refresh
            </button>
          </div>
          <ErrorMessage error={logs.error} />
          {logs.isPending ? (
            <Loading />
          ) : (
            <pre className="service-logs">
              {logs.data?.log || "No log output yet."}
            </pre>
          )}
        </section>
      )}
      {tab === "backups" && s.volume_id && (
        <div className="service-detail-grid">
          <div className="service-detail-main">
            <BackupsPanel
              workspaceId={id!}
              services={list.data?.services || []}
              service={s}
              allowed={allowed}
            />
          </div>
          <aside>
            {allowed && (
              <ScheduleEditor
                key={JSON.stringify(s.backup)}
                service={s}
                disabled={s.status === "restoring"}
              />
            )}
            <BackupStorage workspaceId={id!} />
          </aside>
        </div>
      )}
      {tab === "settings" && allowed && (
        <section className="panel service-settings">
          <ServiceConfig
            key={`${s.version}-${s.external_port}-${JSON.stringify(s.config)}`}
            service={s}
            engine={engine}
            onSave={async (body) => {
              await request(`services/${s.id}`, body, "PATCH");
              await refresh();
              ui.notify("Service settings saved");
            }}
          />
        </section>
      )}
      {open && (
        <ConnectServiceModal
          workspaceId={id!}
          engines={catalog.data || []}
          services={list.data?.services || []}
          projects={editableProjects}
          service={s}
          onClose={() => setOpen(false)}
          onSaved={async () => {
            setChanged(true);
            await refresh();
          }}
        />
      )}
    </div>
  );
}
