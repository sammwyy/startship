import { useProject } from "../../layouts/ProjectLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { remove, request } from "../../services/api";
import type { DomainRoute, WorkspaceDomain } from "../../services/types";
import { text } from "../../lib/project";
import { HostPicker, pickedHost } from "../../components/HostPicker";
import { ProjectHeading } from "../../components/ProjectHeading";
import { Field } from "../../components/ui";
import { Icon } from "../../components/Icon";

export function DomainsView() {
  const { project, containers, routes, refresh } = useProject(),
    { user, can } = useSession(),
    ui = useUI();
  const editable = can("domains.manage", project);
  async function edit(route?: DomainRoute) {
    if (!containers.length) {
      ui.notify("Deploy the project to detect its containers first");
      return;
    }
    const { domains } = await request<{ domains: WorkspaceDomain[] }>(
      `workspaces/${project.workspace_id}/domains`,
    );
    ui.dialog({
      title: route ? "Edit domain" : "Connect domain",
      description:
        "Pick a hostname under one of the workspace domains. Longer path prefixes take priority.",
      submit: route ? "Save domain" : "Connect domain",
      fields: (
        <>
          <HostPicker
            domains={domains}
            host={route?.host}
            admin={user?.platform_admin}
          />
          <Field
            label="Path prefix"
            name="path_prefix"
            defaultValue={route?.path_prefix || "/"}
            required
          />
          {containers.length > 1 ? (
            <label>
              Target container
              <select
                name="container_id"
                defaultValue={route?.container_id || route?.app_id}
              >
                {containers.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name} · :{c.port}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <>
              <input
                type="hidden"
                name="container_id"
                value={containers[0].id}
              />
              <p className="small muted">Target: {containers[0].name}</p>
            </>
          )}
          <label className="inline-check">
            <input
              type="checkbox"
              name="strip_prefix"
              defaultChecked={route?.strip_prefix}
            />
            Strip path prefix before forwarding
          </label>
          <p className="note">
            Point this hostname’s DNS A record to your server’s public IP.
          </p>
        </>
      ),
      onSubmit: async (form) => {
        await request(
          `routes${route ? `/${route.id}` : ""}`,
          {
            host: pickedHost(form),
            project_id: project.id,
            container_id: text(form, "container_id"),
            path_prefix: text(form, "path_prefix"),
            strip_prefix: form.has("strip_prefix"),
          },
          route ? "PATCH" : "POST",
        );
        await refresh();
        ui.notify("Domain saved");
      },
    });
  }
  return (
    <>
      <ProjectHeading title="Domains" />
      <div className="section-head">
        <h2>Domains</h2>
        <button
          className="primary"
          disabled={!editable}
          onClick={() => ui.run(() => edit())}
        >
          <Icon name="plus" />
          Connect domain
        </button>
      </div>
      <div className="panel">
        {routes.map((route) => (
          <div className="container-row" key={route.id}>
            <span className="card-icon">
              <Icon name="globe" />
            </span>
            <div className="container-copy">
              <a
                href={
                  route.url && /^https?:\/\//.test(route.url)
                    ? route.url
                    : undefined
                }
                target="_blank"
                rel="noopener noreferrer"
              >
                {route.host}
                {route.path_prefix}
              </a>
              <span>
                {containers.find(
                  (c) => c.id === (route.container_id || route.app_id),
                )?.name || "Container unavailable"}
                {route.strip_prefix ? " · strips prefix" : ""}
              </span>
            </div>
            <button
              disabled={!editable}
              aria-label={`Edit ${route.host}`}
              onClick={() => ui.run(() => edit(route))}
            >
              Edit
            </button>
            <button
              disabled={!editable}
              aria-label={`Delete ${route.host}`}
              onClick={() =>
                ui.dialog({
                  title: "Disconnect domain",
                  description: `Disconnect ${route.host}${route.path_prefix}?`,
                  submit: "Disconnect",
                  onSubmit: async () => {
                    await remove(`routes/${route.id}`);
                    await refresh();
                  },
                })
              }
            >
              <Icon name="close" />
            </button>
          </div>
        ))}
        {!routes.length && (
          <p className="panel-pad small muted">
            No domains connected. Deploy the project, then point a hostname to
            it.
          </p>
        )}
      </div>
      <p className="note">
        One container is the default target. With multiple containers, choose a
        target for each hostname or path. Longer matching path prefixes take
        priority.
      </p>
    </>
  );
}
