import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Heading } from "../../components/Heading";
import { CatalogRefresh } from "../../components/CatalogRefresh";
import { HostPicker, pickedHost } from "../../components/HostPicker";
import { Icon } from "../../components/Icon";
import { RecipeSchemaFields } from "../../components/RecipeSchemaFields";
import { ServiceModal, ServicePicker } from "../../components/ServiceUI";
import {
  AsyncForm,
  Badge,
  Empty,
  ErrorMessage,
  Field,
  Loading,
} from "../../components/ui";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useLiveSubscription } from "../../providers/LiveProvider";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { remove, request } from "../../services/api";
import {
  certificateCovers,
  useCertificates,
} from "../../services/certificates";
import type {
  Recipe,
  RecipeApp,
  RecipeApps,
  RecipeFile,
  Values,
} from "../../services/recipeApps";
import { filledValues } from "../../services/recipeApps";
import type { WorkspaceDomain } from "../../services/types";
import "./recipeApps.css";

type Ports = Record<string, number>;

function RecipePorts({
  recipe,
  ports,
  change,
}: {
  recipe: Recipe;
  ports: Ports;
  change: (value: Ports) => void;
}) {
  return (
    !!recipe.ports?.length && (
      <div className="recipe-fields">
        {recipe.ports.map((port) => (
          <Field
            key={port.id}
            label={`${port.id} · ${port.port}/${port.protocol.toUpperCase()}`}
            type="number"
            min={0}
            max={65535}
            required
            value={ports[port.id] ?? port.host_port}
            onChange={(event) =>
              change({ ...ports, [port.id]: Number(event.target.value) })
            }
          />
        ))}
        <p className="small muted">
          Host ports are public. Set a port to 0 to keep it private.
        </p>
      </div>
    )
  );
}

function InstallApp({
  path,
  workspaceId,
  close,
  installed,
}: {
  path: string;
  workspaceId: string;
  close: () => void;
  installed: (app: RecipeApp) => void;
}) {
  const catalog = useQuery({
    queryKey: ["recipes"],
    queryFn: ({ signal }) =>
      request<Recipe[]>("recipes", undefined, "GET", signal),
  });
  const [recipe, setRecipe] = useState<Recipe>();
  const [config, setConfig] = useState<Values>({});
  const [ports, setPorts] = useState<Ports>({});
  return (
    <ServiceModal
      className="catalog-modal"
      title={recipe ? `Install ${recipe.name}` : "App recipes"}
      eyebrow={recipe ? "02 / INSTALL SETTINGS" : "01 / APP CATALOG"}
      description={
        recipe?.description ||
        "Install self-hosted software without setting up a project."
      }
      onClose={close}
    >
      {recipe ? (
        <AsyncForm
          key={recipe.id}
          className="recipe-install-form"
          onSubmit={async (form) => {
            const app = await request<RecipeApp>(path, {
              name: form.get("name"),
              recipe: recipe.id,
              config: filledValues(recipe.config_schema, config),
              ports,
            });
            installed(app);
          }}
        >
          <div className="service-config-body">
            <Field
              name="name"
              label="App name"
              defaultValue={recipe.id}
              pattern="[a-z][a-z0-9_\-]{0,62}"
              required
            />
            <div className="recipe-fields">
              <RecipeSchemaFields
                schema={recipe.config_schema}
                values={config}
                change={setConfig}
              />
            </div>
            <RecipePorts recipe={recipe} ports={ports} change={setPorts} />
          </div>
          <div className="service-modal-foot">
            <button type="button" onClick={() => setRecipe(undefined)}>
              Back to recipes
            </button>
            <button className="primary">Install app</button>
          </div>
        </AsyncForm>
      ) : (
        <div className="catalog-scroll">
          <CatalogRefresh path="recipes" workspaceId={workspaceId} />
          <ErrorMessage error={catalog.error} />
          {catalog.isPending && <Loading />}
          <div className="catalog-grid">
            {catalog.data?.map((entry) => (
              <button
                key={entry.id}
                className="catalog-tile"
                onClick={() => {
                  setConfig({});
                  setPorts({});
                  setRecipe(entry);
                }}
              >
                <span className="catalog-tile-top">
                  <Icon name={entry.icon} />
                  <strong>{entry.name}</strong>
                  <Icon name="next" />
                </span>
                <span className="catalog-description">{entry.description}</span>
                <span className="catalog-versions">Recipe {entry.version}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </ServiceModal>
  );
}

function FileEditor({
  file,
  path,
  running,
  onSaved,
}: {
  file: RecipeFile;
  path: string;
  running: boolean;
  onSaved: () => Promise<unknown>;
}) {
  const ui = useUI();
  const entry = useQuery({
    queryKey: [path],
    queryFn: () => request<Values>(path),
    retry: false,
  });
  const [draft, setDraft] = useState<Values>();
  return (
    <>
      <ErrorMessage error={entry.error} />
      {entry.isPending ? (
        <Loading />
      ) : (
        <AsyncForm
          className="recipe-editor"
          onSubmit={async (data) => {
            const restart = data.get("restart") === "on";
            await request(
              path,
              {
                values: filledValues(file.schema, draft ?? entry.data ?? {}),
                restart,
              },
              "PUT",
            );
            await onSaved();
            ui.notify(
              restart ? "File saved; app restarting" : "Config file saved",
            );
          }}
        >
          <div className="recipe-fields">
            <RecipeSchemaFields
              schema={file.schema}
              values={draft ?? entry.data ?? {}}
              change={setDraft}
            />
          </div>
          <div className="recipe-actions">
            {running && (
              <label className="recipe-check">
                <input type="checkbox" name="restart" /> Restart after saving
              </label>
            )}
            <button className="primary">Save file</button>
          </div>
        </AsyncForm>
      )}
    </>
  );
}

function EditApp({
  app,
  path,
  close,
  refresh,
}: {
  app: RecipeApp;
  path: string;
  close: () => void;
  refresh: () => Promise<unknown>;
}) {
  const ui = useUI();
  const [fileId, setFile] = useState("");
  const [config, setConfig] = useState<Values>(app.config || {});
  const [ports, setPorts] = useState<Ports>(app.ports);
  const file = app.recipe.files?.find((f) => f.id === fileId);
  return (
    <ServiceModal
      title="App configuration"
      eyebrow={app.recipe.name.toUpperCase()}
      description={`Install settings and recipe-declared files for ${app.name}.`}
      onClose={close}
    >
      <div className="service-config-body">
        {!!app.recipe.files?.length && (
          <ServicePicker
            label="Configuration source"
            value={fileId}
            onChange={setFile}
            options={[
              { value: "", label: "Install settings" },
              ...app.recipe.files.map((f) => ({ value: f.id, label: f.path })),
            ]}
          />
        )}
        {file ? (
          <>
            <p className="small muted">
              <code>
                {file.volume}/{file.path}
              </code>{" "}
              · {file.format}
            </p>
            <FileEditor
              key={file.id}
              file={file}
              path={`${path}/files/${file.id}`}
              running={app.running}
              onSaved={async () => {
                await refresh();
                close();
              }}
            />
          </>
        ) : (
          <AsyncForm
            className="recipe-editor"
            onSubmit={async () => {
              await request(
                `${path}/config`,
                {
                  config: filledValues(app.recipe.config_schema, config),
                  ports,
                },
                "PATCH",
              );
              await refresh();
              ui.notify("Install settings saved");
              close();
            }}
          >
            <div className="recipe-fields">
              <RecipeSchemaFields
                schema={app.recipe.config_schema}
                values={config}
                change={setConfig}
              />
            </div>
            <RecipePorts recipe={app.recipe} ports={ports} change={setPorts} />
            {!Object.keys(app.recipe.config_schema.properties || {}).length &&
              !app.recipe.ports?.length && (
                <p className="muted">This recipe has no install settings.</p>
              )}
            <div className="recipe-actions">
              <p className="small muted">
                Saving recreates running containers and preserves volumes.
              </p>
              <button className="primary">Save settings</button>
            </div>
          </AsyncForm>
        )}
      </div>
    </ServiceModal>
  );
}

function BindDomain({
  app,
  path,
  workspaceId,
  close,
  refresh,
}: {
  app: RecipeApp;
  path: string;
  workspaceId: string;
  close: () => void;
  refresh: () => Promise<unknown>;
}) {
  const { workspace } = useDashboard();
  const { user, can } = useSession();
  const admin = !!user?.platform_admin;
  const certificates = useCertificates(
    workspaceId,
    admin || can("workspace.domains", workspace),
  );
  const domainsPath = `workspaces/${workspaceId}/domains`;
  const domains = useQuery({
    queryKey: [domainsPath],
    queryFn: () => request<{ domains: WorkspaceDomain[] }>(domainsPath),
  });
  const [host, setHost] = useState("");
  const [endpoint, setEndpoint] = useState(app.recipe.http?.[0]?.id ?? "");
  const [certificate, setCertificate] = useState("");
  const matches =
    certificates.data?.certificates.filter((c) => certificateCovers(c, host)) ||
    [];
  return (
    <ServiceModal
      title="Connect domain"
      eyebrow="HTTP ROUTING"
      description="Send web traffic to a recipe endpoint without publishing a host port."
      onClose={close}
    >
      <div className="service-config-body">
        <ErrorMessage error={domains.error} />
        {domains.isPending ? (
          <Loading />
        ) : (
          <AsyncForm
            className="recipe-editor"
            onSubmit={async (form) => {
              await request(`${path}/domains`, {
                endpoint,
                host: pickedHost(form),
                certificate_id: matches.some((c) => c.id === certificate)
                  ? certificate
                  : "",
                force_https: form.get("https") === "on",
              });
              await refresh();
              close();
            }}
          >
            <ServicePicker
              label="HTTP endpoint"
              value={endpoint}
              onChange={setEndpoint}
              options={(app.recipe.http || []).map((e) => ({
                value: e.id,
                label: `${e.id} · ${e.container}:${e.port}`,
              }))}
            />
            <HostPicker
              domains={domains.data?.domains || []}
              admin={admin}
              onHostChange={setHost}
            />
            {!!matches.length && (
              <ServicePicker
                label="Certificate"
                value={certificate}
                onChange={setCertificate}
                options={[
                  { value: "", label: "Automatic" },
                  ...matches.map((c) => ({
                    value: c.id,
                    label: c.domains.join(", "),
                  })),
                ]}
              />
            )}
            <label className="recipe-check">
              <input type="checkbox" name="https" /> Redirect HTTP to HTTPS
            </label>
            <div className="recipe-actions">
              <button
                className="primary"
                disabled={!admin && !domains.data?.domains?.length}
              >
                Connect
              </button>
            </div>
          </AsyncForm>
        )}
      </div>
    </ServiceModal>
  );
}

function AppDetail({
  app,
  collection,
  base,
  workspaceId,
  manage,
  refresh,
}: {
  app: RecipeApp;
  collection: string;
  base: string;
  workspaceId: string;
  manage: boolean;
  refresh: () => Promise<unknown>;
}) {
  const [dialog, setDialog] = useState<"settings" | "domain" | "delete">();
  const [acting, setActing] = useState(false);
  const ui = useUI();
  const navigate = useNavigate();
  const path = `${collection}/${app.id}`;
  const disabled = acting || !!app.action;
  const close = () => setDialog(undefined);
  function action(value: string) {
    ui.run(async () => {
      setActing(true);
      try {
        await request(`${path}/${value}`, undefined, "POST");
        await refresh();
      } finally {
        setActing(false);
      }
    });
  }
  return (
    <>
      <Link to={base} className="recipe-back">
        <Icon name="back" /> Apps
      </Link>
      <Heading
        title={app.name}
        kicker={`${app.recipe.name} / RECIPE ${app.recipe.version}`}
        subtitle={app.recipe.description}
        actions={
          manage && (
            <button
              className="primary"
              disabled={disabled}
              onClick={() => setDialog("settings")}
            >
              <Icon name="settings" /> Configure
            </button>
          )
        }
      />
      <ErrorMessage error={app.error} />
      <div className="panel recipe-runtime">
        <div>
          <span className="eyebrow">APP RUNTIME</span>
          <Badge status={app.status} />
        </div>
        {manage && (
          <div className="recipe-actions">
            <button
              disabled={disabled}
              onClick={() => action(app.running ? "stop" : "start")}
            >
              {app.running ? "Stop" : "Start"}
            </button>
            <button
              disabled={disabled || !app.running}
              onClick={() => action("restart")}
            >
              Restart
            </button>
            <button disabled={disabled} onClick={() => action("update")}>
              Update images
            </button>
            <button
              disabled={disabled}
              onClick={() => setDialog("delete")}
              aria-label="Delete app"
            >
              <Icon name="trash" />
            </button>
          </div>
        )}
      </div>
      <div className="recipe-detail-grid">
        <section className="panel recipe-section">
          <h2>
            <Icon name="layers" /> Containers
          </h2>
          {app.recipe.containers.map((c) => {
            const container = app.containers[c.id];
            const status = container?.status || "pending";
            return (
              <div className="recipe-row" key={c.id}>
                <div>
                  <strong>{c.id}</strong>
                  <code>{c.image}</code>
                </div>
                <Badge
                  status={
                    status === "running" ? container?.health || status : status
                  }
                />
              </div>
            );
          })}
        </section>
        <section className="panel recipe-section">
          <h2>
            <Icon name="hard-drive" /> Persistent volumes
          </h2>
          {app.recipe.volumes?.length ? (
            app.recipe.volumes.map((v) => (
              <div className="recipe-row" key={v}>
                <strong>{v}</strong>
                <span className="muted small">App-owned</span>
              </div>
            ))
          ) : (
            <p className="muted small">No persistent volumes.</p>
          )}
        </section>
        <section className="panel recipe-section">
          <h2>
            <Icon name="plug" /> Public ports
          </h2>
          {app.recipe.ports?.length ? (
            app.recipe.ports.map((p) => (
              <div className="recipe-row" key={p.id}>
                <div>
                  <strong>{p.id}</strong>
                  <code>
                    {p.container}:{p.port}/{p.protocol}
                  </code>
                </div>
                <code>{app.ports[p.id] || "Not published"}</code>
              </div>
            ))
          ) : (
            <p className="muted small">No ports published to the host.</p>
          )}
        </section>
        <section className="panel recipe-section">
          <h2>
            <Icon name="globe" /> Domains
          </h2>
          {app.domains.map((domain) => (
            <div className="recipe-row" key={domain.id}>
              <div>
                <a
                  href={`${domain.force_https || domain.certificate_id ? "https" : "http"}://${domain.host}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  {domain.host}
                </a>
                <code>{domain.endpoint}</code>
              </div>
              {manage && (
                <button
                  disabled={disabled}
                  className="icon-button"
                  aria-label={`Disconnect ${domain.host}`}
                  onClick={() =>
                    ui.run(async () => {
                      await remove(`${path}/domains/${domain.id}`);
                      await refresh();
                    })
                  }
                >
                  <Icon name="trash" />
                </button>
              )}
            </div>
          ))}
          {!app.domains.length && (
            <p className="muted small">No domains connected.</p>
          )}
          {manage && !!app.recipe.http?.length && (
            <button disabled={disabled} onClick={() => setDialog("domain")}>
              <Icon name="plus" /> Connect domain
            </button>
          )}
        </section>
      </div>
      {dialog === "settings" && (
        <EditApp app={app} path={path} refresh={refresh} close={close} />
      )}
      {dialog === "domain" && (
        <BindDomain
          app={app}
          path={path}
          workspaceId={workspaceId}
          refresh={refresh}
          close={close}
        />
      )}
      {dialog === "delete" && (
        <ServiceModal
          title={`Delete ${app.name}?`}
          description="Containers, network and routes are removed. Volumes are kept unless you purge them."
          onClose={close}
        >
          <div className="service-config-body">
            <AsyncForm
              className="recipe-editor"
              onSubmit={async (form) => {
                await remove(
                  path + (form.get("purge") === "on" ? "?purge=true" : ""),
                );
                await refresh();
                navigate(base);
              }}
            >
              <label className="recipe-check">
                <input type="checkbox" name="purge" /> Permanently purge the app
                volumes
              </label>
              <div className="recipe-actions">
                <button type="button" onClick={close}>
                  Cancel
                </button>
                <button className="danger">Delete app</button>
              </div>
            </AsyncForm>
          </div>
        </ServiceModal>
      )}
    </>
  );
}

export function RecipeAppsView() {
  const { workspaceId = "", appId } = useParams();
  const { workspace } = useDashboard();
  const client = useQueryClient();
  const navigate = useNavigate();
  const collection = `workspaces/${workspaceId}/apps`;
  const base = `/workspace/${workspaceId}/apps`;
  const query = useQuery({
    queryKey: [collection],
    queryFn: ({ signal }) =>
      request<RecipeApps>(collection, undefined, "GET", signal),
  });
  const liveError = useLiveSubscription({
    topic: "recipe_apps",
    workspace_id: workspaceId,
  });
  useLiveSubscription(appId ? null : { topic: "recipes_catalog" });
  useQuery({
    queryKey: ["recipes"],
    queryFn: ({ signal }) =>
      request<Recipe[]>("recipes", undefined, "GET", signal),
    enabled: !appId,
  });
  const [install, setInstall] = useState(false);
  const refresh = () => client.invalidateQueries({ queryKey: [collection] });
  const manage = !!query.data?.can_manage;
  const apps = query.data?.apps || [];
  const app = apps.find((item) => item.id === appId);
  return (
    <div className="services-page recipe-page">
      {!appId && (
        <Heading
          title="Apps"
          kicker={workspace?.name}
          subtitle="Self-hosted software on private networks with its own persistent storage."
          actions={
            manage && (
              <button className="primary" onClick={() => setInstall(true)}>
                <Icon name="plus" /> Install app
              </button>
            )
          }
        />
      )}
      <ErrorMessage error={query.error || liveError} />
      {query.isPending ? (
        <Loading />
      ) : appId ? (
        app ? (
          <AppDetail
            key={app.id}
            app={app}
            collection={collection}
            base={base}
            workspaceId={workspaceId}
            manage={manage}
            refresh={refresh}
          />
        ) : (
          <Empty title="App not found">
            <Link to={base}>Back to apps</Link>
          </Empty>
        )
      ) : apps.length ? (
        <>
          <div className="service-list-meta">
            <span>{apps.length} installed</span>
            <span>
              <i className="live-dot" /> Live updates
            </span>
          </div>
          <div className="panel recipe-list">
            <div className="recipe-list-head">
              <span>App</span>
              <span>Status</span>
              <span>Containers</span>
              <span>Domains</span>
            </div>
            {apps.map((item) => (
              <Link
                className="recipe-list-row"
                to={`${base}/${item.id}`}
                key={item.id}
              >
                <div className="service-identity">
                  <span className="card-icon">
                    <Icon name={item.recipe.icon} />
                  </span>
                  <div>
                    <strong>{item.name}</strong>
                    <small>
                      {item.recipe.name} · {item.recipe.version}
                    </small>
                  </div>
                </div>
                <Badge status={item.status} />
                <span>{item.recipe.containers.length}</span>
                <span>{item.domains.length}</span>
              </Link>
            ))}
          </div>
        </>
      ) : (
        <Empty title="No apps installed">
          <p>
            Start with a recipe for media, games, voice chat or Git hosting.
          </p>
          {manage && (
            <button onClick={() => setInstall(true)}>Browse recipes</button>
          )}
        </Empty>
      )}
      {install && (
        <InstallApp
          path={collection}
          workspaceId={workspaceId}
          close={() => setInstall(false)}
          installed={(item) => {
            client.setQueryData<RecipeApps>(
              [collection],
              (old) =>
                old && {
                  ...old,
                  apps: [
                    ...(old.apps || []).filter((a) => a.id !== item.id),
                    item,
                  ],
                },
            );
            setInstall(false);
            navigate(`${base}/${item.id}`);
            void refresh();
          }}
        />
      )}
    </div>
  );
}
