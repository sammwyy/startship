import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { request, remove } from "../../services/api";
import {
  endpointHost,
  sameEndpoint,
  useConnections,
  useNetworks,
} from "../../services/services";
import type { WorkspaceNetwork } from "../../services/types";
import { Heading } from "../../components/Heading";
import { Icon } from "../../components/Icon";
import { Empty, ErrorMessage, Loading } from "../../components/ui";
import { ServicePicker } from "../../components/ServiceUI";
import {
  resourceHost,
  useWorkspaceResources,
} from "../../components/ConnectionsPanel";

const validName = (v: string) => /^[a-z][a-z0-9-]{0,62}$/.test(v);

function NetworkCard({
  network,
  manage,
}: {
  network: WorkspaceNetwork;
  manage: boolean;
}) {
  const ui = useUI(),
    { resources, find } = useWorkspaceResources(network.workspace_id),
    [member, setMember] = useState("");
  const candidates = resources.filter(
    (r) => !network.members.some((m) => sameEndpoint(m, r)),
  );
  function rename() {
    ui.dialog({
      title: `Rename ${network.name}`,
      fields: (
        <input
          name="name"
          aria-label="Network name"
          defaultValue={network.name}
          required
          pattern="[a-z][a-z0-9-]{0,62}"
        />
      ),
      submit: "Rename",
      onSubmit: (form) =>
        request(`networks/${network.id}`, { name: form.get("name") }, "PATCH"),
    });
  }
  return (
    <section className="panel service-panel" aria-label={network.name}>
      <div className="service-section-head">
        <h2>
          <Icon name="share" /> {network.name}
        </h2>
        {manage && (
          <div className="row">
            <button
              className="icon-button"
              aria-label={`Rename ${network.name}`}
              onClick={rename}
            >
              <Icon name="settings" />
            </button>
            <button
              className="icon-button danger"
              aria-label={`Delete ${network.name}`}
              onClick={() =>
                ui.dialog({
                  title: `Delete ${network.name}?`,
                  description:
                    "Members keep running and keep their own connections, but stop reaching each other through this network.",
                  submit: "Delete network",
                  onSubmit: () => remove(`networks/${network.id}`),
                })
              }
            >
              <Icon name="trash" />
            </button>
          </div>
        )}
      </div>
      {network.members.length ? (
        network.members.map((m) => {
          const r = find(m);
          return (
            <div className="attached-project-row" key={`${m.kind}:${m.id}`}>
              <Icon
                name={
                  m.kind === "service"
                    ? "database"
                    : m.kind === "app"
                      ? "box"
                      : "grid"
                }
              />
              <Link to={r?.href || "#"}>
                <strong>{r?.name || m.id}</strong>
                <span>{r ? endpointHost(m.kind, r.name) : m.kind}</span>
              </Link>
              {manage && (
                <button
                  className="icon-button"
                  aria-label={`Remove ${r?.name || m.id}`}
                  onClick={() =>
                    ui.run(() =>
                      remove(
                        `networks/${network.id}/members/${m.kind}/${m.id}`,
                      ),
                    )
                  }
                >
                  <Icon name="close" />
                </button>
              )}
            </div>
          );
        })
      ) : (
        <p className="muted small">
          No members yet. Members reach each other by hostname.
        </p>
      )}
      {manage && candidates.length > 0 && (
        <div className="row">
          <ServicePicker
            label="Add member"
            value={member}
            options={candidates.map((r) => ({
              value: `${r.kind}:${r.id}`,
              label: `${r.name} (${r.kind})`,
            }))}
            onChange={setMember}
          />
          <button
            disabled={!member}
            onClick={() =>
              ui.run(async () => {
                const [kind, id] = member.split(":");
                await request(`networks/${network.id}/members`, { kind, id });
                setMember("");
                ui.notify(
                  "Joined. Hostnames resolve on the running containers now.",
                );
              })
            }
          >
            <Icon name="plus" />
            Add
          </button>
        </div>
      )}
    </section>
  );
}

export function WorkspaceNetworksView() {
  const id = useParams().workspaceId!,
    { workspace } = useDashboard(),
    { can } = useSession(),
    ui = useUI(),
    networks = useNetworks(id),
    connections = useConnections(id),
    { find } = useWorkspaceResources(id),
    manage = can("networks.manage", workspace);
  function create() {
    ui.dialog({
      title: "New network",
      description:
        "A shared private network. Projects, apps and services you add reach each other by hostname.",
      fields: (
        <input
          name="name"
          aria-label="Network name"
          placeholder="e.g. backend"
          required
          pattern="[a-z][a-z0-9-]{0,62}"
        />
      ),
      submit: "Create network",
      onSubmit: async (form) => {
        const name = String(form.get("name") || "");
        if (!validName(name))
          throw new Error(
            "Use a lowercase name starting with a letter: letters, numbers or -.",
          );
        await request(`workspaces/${id}/networks`, { name });
      },
    });
  }
  const links = connections.data?.connections || [];
  return (
    <div className="services-page">
      <Heading
        title="Networks"
        subtitle="Everything is isolated by default. Connect two resources one-to-one, or group several in a named network."
        actions={
          manage && (
            <button className="primary" onClick={create}>
              <Icon name="plus" />
              New network
            </button>
          )
        }
      />
      <ErrorMessage
        error={
          networks.error ||
          networks.liveError ||
          connections.error ||
          connections.liveError
        }
      />
      {networks.isPending ? (
        <Loading layout="cards" label="Loading networks…" />
      ) : !networks.data?.networks.length ? (
        <Empty title="No shared networks">
          <p>
            Create a network to let several projects, apps and services talk to
            each other by hostname.
          </p>
          {manage && <button onClick={create}>New network</button>}
        </Empty>
      ) : (
        <div className="connection-cards">
          {networks.data.networks.map((n) => (
            <NetworkCard key={n.id} network={n} manage={manage} />
          ))}
        </div>
      )}
      <section className="panel service-panel">
        <div className="service-section-head">
          <h2>
            <Icon name="plug" /> Connections
          </h2>
        </div>
        {links.length ? (
          links.map((c) => (
            <div className="attached-project-row" key={c.id}>
              <Icon name="plug" />
              <Link to={find(c.from)?.href || "#"}>
                <strong>
                  {find(c.from)?.name || c.from.id} →{" "}
                  {find(c.to)?.name || c.to.id}
                </strong>
                <span>{resourceHost(c)}</span>
              </Link>
            </div>
          ))
        ) : (
          <p className="muted small">
            No one-to-one connections yet. Open a project, app or service and
            choose Connect.
          </p>
        )}
      </section>
    </div>
  );
}
