import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import type { Connection, Endpoint, Service } from "../services/types";
import type { RecipeApp } from "../services/recipeApps";
import { queries } from "../services/queries";
import {
  connectionEnv,
  endpointHost,
  envPrefix,
  sameEndpoint,
  useConnections,
  useServices,
  validServiceName,
} from "../services/services";
import { remove, request } from "../services/api";
import { useUI } from "../providers/UIProvider";
import { EnvPreview, ServiceModal } from "./ServiceUI";
import { AsyncForm, ErrorMessage, Field } from "./ui";
import { Icon } from "./Icon";

export interface WorkspaceResource extends Endpoint {
  name: string;
  detail: string;
  href: string;
  service?: Service;
}

const kindLabel = { project: "Project", app: "App", service: "Service" };
const kindIcon = { project: "grid", app: "box", service: "database" };

export function useWorkspaceResources(workspaceId: string) {
  const projects = useQuery(queries.projects(workspaceId)),
    services = useServices(workspaceId),
    apps = useQuery({
      queryKey: [`workspaces/${workspaceId}/apps`],
      queryFn: ({ signal }) =>
        request<{ apps: RecipeApp[] }>(
          `workspaces/${workspaceId}/apps`,
          undefined,
          "GET",
          signal,
        ),
    });
  const list = <T,>(v: T[] | undefined) => (Array.isArray(v) ? v : []);
  const resources: WorkspaceResource[] = [
    ...list(projects.data).map((p) => ({
      kind: "project" as const,
      id: p.id,
      name: p.name,
      detail: "Project",
      href: `/project/${p.id}/connections`,
    })),
    ...list(apps.data?.apps).map((a) => ({
      kind: "app" as const,
      id: a.id,
      name: a.name,
      detail: a.recipe.name,
      href: `/workspace/${workspaceId}/apps/${a.id}`,
    })),
    ...list(services.data?.services).map((s) => ({
      kind: "service" as const,
      id: s.id,
      name: s.name,
      detail: `${s.engine} · ${s.version}`,
      href: `/workspace/${workspaceId}/services/${s.id}`,
      service: s,
    })),
  ];
  return {
    resources,
    find: (e: Endpoint) => resources.find((r) => sameEndpoint(r, e)),
    error: projects.error || services.error || apps.error,
    pending: projects.isPending || services.isPending || apps.isPending,
  };
}

export function resourceHost(c: Connection) {
  return c.to.kind === "service" ? `${c.alias}.local` : c.alias;
}

export function ConnectionsPanel({
  workspaceId,
  endpoint,
  canManage,
  onChanged,
}: {
  workspaceId: string;
  endpoint: Endpoint;
  canManage: boolean;
  onChanged?: () => void;
}) {
  const ui = useUI(),
    connections = useConnections(workspaceId),
    { find, error } = useWorkspaceResources(workspaceId),
    [open, setOpen] = useState(false);
  const all = connections.data?.connections || [],
    uses = all.filter((c) => sameEndpoint(c.from, endpoint)),
    usedBy = all.filter((c) => sameEndpoint(c.to, endpoint));
  function disconnect(c: Connection) {
    ui.dialog({
      title: `Disconnect ${c.alias}?`,
      description:
        "Both resources keep running. The consumer loses the hostname and environment variables on its next deploy or restart.",
      submit: "Disconnect",
      onSubmit: async () => {
        await remove(`connections/${c.id}`);
        onChanged?.();
      },
    });
  }
  function row(c: Connection, other: Endpoint, outgoing: boolean) {
    const r = find(other);
    return (
      <div className="panel connection-card" key={c.id}>
        <div className="connection-card-head">
          <div className="service-identity">
            <Icon name={kindIcon[other.kind]} />
            <span>
              {r ? (
                <Link to={r.href}>
                  <strong>{r.name}</strong>
                </Link>
              ) : (
                <strong>{other.id}</strong>
              )}
              <span>
                {kindLabel[other.kind]} ·{" "}
                {outgoing ? "reached at " : "reaches this as "}
                <span className="attachment-chip">{resourceHost(c)}</span>
              </span>
            </span>
          </div>
          {canManage && (
            <button
              className="icon-button danger"
              aria-label={`Disconnect ${c.alias}`}
              onClick={() => disconnect(c)}
            >
              <Icon name="close" />
            </button>
          )}
        </div>
        {outgoing && Object.keys(c.env).length > 0 && (
          <EnvPreview env={c.env} canReveal={canManage} />
        )}
      </div>
    );
  }
  return (
    <section className="connections-panel">
      <div className="service-section-head">
        <h2>Connections</h2>
        {canManage && (
          <button onClick={() => setOpen(true)}>
            <Icon name="plug" />
            New connection
          </button>
        )}
      </div>
      <ErrorMessage
        error={connections.error || connections.liveError || error}
      />
      {!uses.length && !usedBy.length ? (
        <p className="muted small">
          Isolated. Connect it to a project, app or service to share a private
          network with internal DNS.
        </p>
      ) : (
        <div className="connection-cards">
          {uses.map((c) => row(c, c.to, true))}
          {usedBy.map((c) => row(c, c.from, false))}
        </div>
      )}
      {open && (
        <ConnectModal
          workspaceId={workspaceId}
          endpoint={endpoint}
          connections={all}
          onClose={() => setOpen(false)}
          onSaved={() => onChanged?.()}
        />
      )}
    </section>
  );
}

function aliasFor(name: string) {
  const alias = endpointHost("project", name).replace(/^[^a-z]+/, "");
  return alias.slice(0, 63) || "link";
}

export function ConnectModal({
  workspaceId,
  endpoint,
  connections,
  onClose,
  onSaved,
}: {
  workspaceId: string;
  endpoint: Endpoint;
  connections: Connection[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const { resources, find } = useWorkspaceResources(workspaceId),
    engines = useQuery(queries.catalog()),
    [outgoing, setOutgoing] = useState(endpoint.kind !== "service"),
    [selected, setSelected] = useState<Endpoint>(),
    [alias, setAlias] = useState("");
  const other = selected && find(selected),
    self = find(endpoint),
    from = outgoing ? endpoint : selected,
    to = outgoing ? selected : endpoint,
    provider = outgoing ? other : self;
  const taken = (e: Endpoint) =>
    connections.some((c) =>
      outgoing
        ? sameEndpoint(c.from, endpoint) && sameEndpoint(c.to, e)
        : sameEndpoint(c.from, e) && sameEndpoint(c.to, endpoint),
    );
  const conflict =
    !!from &&
    connections.some(
      (c) =>
        sameEndpoint(c.from, from) && envPrefix(c.alias) === envPrefix(alias),
    );
  const engine =
    provider?.service &&
    (provider.service.definition ||
      engines.data?.find((e) => e.id === provider.service?.engine));
  const env =
    provider?.service && engine
      ? connectionEnv(provider.service, engine, alias)
      : { [`${envPrefix(alias)}_HOST`]: alias };
  const valid = !!selected && validServiceName(alias) && !conflict;
  return (
    <ServiceModal
      title={`Connect ${self?.name || "resource"}`}
      description="Creates a private bridge network between the two resources only. Everything else stays isolated."
      onClose={onClose}
    >
      <AsyncForm
        onSubmit={async () => {
          await request(`workspaces/${workspaceId}/connections`, {
            from,
            to,
            alias,
          });
          onSaved();
          onClose();
        }}
      >
        <div className="service-config-body">
          <div className="segmented" role="group" aria-label="Direction">
            <button
              type="button"
              className={outgoing ? "selected" : ""}
              aria-pressed={outgoing}
              onClick={() => setOutgoing(true)}
            >
              {self?.name || "This"} uses…
            </button>
            <button
              type="button"
              className={!outgoing ? "selected" : ""}
              aria-pressed={!outgoing}
              onClick={() => setOutgoing(false)}
            >
              …uses {self?.name || "this"}
            </button>
          </div>
          <div
            className="connect-options"
            role="group"
            aria-label="Workspace resources"
          >
            {resources
              .filter((r) => !sameEndpoint(r, endpoint))
              .map((r) => (
                <button
                  type="button"
                  key={`${r.kind}:${r.id}`}
                  className={`connect-option ${selected && sameEndpoint(selected, r) ? "selected" : ""}`}
                  disabled={taken(r)}
                  onClick={() => {
                    setSelected({ kind: r.kind, id: r.id });
                    setAlias(aliasFor(outgoing ? r.name : self?.name || ""));
                  }}
                >
                  <Icon name={kindIcon[r.kind]} />
                  <span>
                    <strong>{r.name}</strong>
                    <span>
                      {kindLabel[r.kind]} · {r.detail}
                    </span>
                    {taken(r) && <span>Already connected</span>}
                  </span>
                  {selected && sameEndpoint(selected, r) && (
                    <Icon name="check" />
                  )}
                </button>
              ))}
          </div>
          <Field
            label="Hostname alias"
            name="alias"
            required
            disabled={!selected}
            value={alias}
            maxLength={63}
            onChange={(e) => setAlias(e.target.value)}
            placeholder="e.g. primary_db"
            aria-invalid={!!alias && (!validServiceName(alias) || conflict)}
          />
          <span className="small muted">
            The consumer reaches the other side at this name and gets{" "}
            <code>{envPrefix(alias || "alias")}_*</code> variables on its next
            deploy or restart.
          </span>
          {alias && (!validServiceName(alias) || conflict) && (
            <span className="service-validation">
              {conflict
                ? "This alias or environment prefix is already in use."
                : "Start with a lowercase letter. Use up to 63 lowercase letters, numbers, _ or -."}
            </span>
          )}
          {selected && alias && <EnvPreview env={env} canReveal />}
        </div>
        <div className="service-modal-foot">
          <button type="button" onClick={onClose}>
            Cancel
          </button>
          <button className="primary" disabled={!valid}>
            <Icon name="plug" />
            Connect
          </button>
        </div>
      </AsyncForm>
    </ServiceModal>
  );
}
