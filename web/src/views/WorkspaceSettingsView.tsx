import { useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { useDashboard } from "../layouts/DashboardLayout";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { remove, request } from "../services/api";
import type { WorkspaceDomain } from "../services/types";
import { queries } from "../services/queries";
import { text } from "../lib/project";
import { Heading } from "../components/Heading";
import { AsyncForm, Empty, Field } from "../components/ui";
import { domainPatterns } from "../components/HostPicker";
import { Icon } from "../components/Icon";

export function WorkspaceSettingsView() {
  const id = useParams().workspaceId!,
    { workspace } = useDashboard(),
    { can } = useSession(),
    ui = useUI(),
    client = useQueryClient();
  const path = `workspaces/${id}/domains`;
  const { data } = useSuspenseQuery({
    queryKey: [path],
    queryFn: () =>
      request<{ domains: WorkspaceDomain[]; can_manage: boolean }>(path),
  });
  const refresh = () => client.invalidateQueries({ queryKey: [path] });
  return (
    <>
      <Heading
        kicker="Workspace"
        title="Workspace settings"
        subtitle="A shared home for your projects."
      />
      <AsyncForm
        key={workspace?.id + (workspace?.name || "")}
        className="panel panel-pad workspace-settings"
        disabled={!can("workspace.manage", workspace)}
        onSubmit={async (form) => {
          await request(
            `workspaces/${id}`,
            { name: text(form, "name") },
            "PATCH",
          );
          await client.invalidateQueries(queries.workspaces());
          ui.notify("Workspace saved");
        }}
      >
        <Field
          label="Workspace name"
          name="name"
          defaultValue={workspace?.name}
          required
        />
        <button className="primary">Save changes</button>
      </AsyncForm>
      <section className="workspace-domains">
        <div className="section-head">
          <div>
            <h2>Domains</h2>
            <p className="small muted">
              Projects in this workspace can only route hostnames under these
              domains.
            </p>
          </div>
        </div>
        {data.domains.length ? (
          <div className="panel table-panel">
            <table className="domain-table">
              <thead>
                <tr>
                  <th>Domain</th>
                  <th>Allows</th>
                  <th>Routes</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {data.domains.map((d) => (
                  <tr key={d.id}>
                    <td>
                      <span className="domain-name">
                        <Icon name="globe" />
                        {d.name}
                      </span>
                    </td>
                    <td>
                      <div className="chips">
                        {domainPatterns(d).map((p) => (
                          <code className="domain-pattern" key={p}>
                            {p}
                          </code>
                        ))}
                      </div>
                    </td>
                    <td>{d.routes}</td>
                    <td className="row-actions">
                      {data.can_manage && (
                        <button
                          className="danger"
                          onClick={() =>
                            ui.dialog({
                              title: `Remove ${d.name}`,
                              description:
                                "Projects will no longer be able to connect hostnames under this domain.",
                              submit: "Remove domain",
                              onSubmit: async () => {
                                await remove(`${path}/${d.id}`);
                                await refresh();
                                ui.notify("Domain removed");
                              },
                            })
                          }
                        >
                          <Icon name="trash" />
                          Remove
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty title="No domains yet">
            A workspace administrator adds the domains projects can use.
          </Empty>
        )}
        {data.can_manage && (
          <AsyncForm
            className="panel panel-pad domain-form"
            onSubmit={async (form, element) => {
              await request(path, {
                name: text(form, "name"),
                subdomains: form.has("subdomains"),
              });
              element.reset();
              await refresh();
              ui.notify("Domain added");
            }}
          >
            <Field
              label="Domain"
              name="name"
              required
              placeholder="example.com or *.example.com"
              autoComplete="off"
            />
            <label className="inline-check">
              <input type="checkbox" name="subdomains" />
              Also allow any subdomain
            </label>
            <button className="primary">
              <Icon name="plus" />
              Add domain
            </button>
          </AsyncForm>
        )}
      </section>
    </>
  );
}
