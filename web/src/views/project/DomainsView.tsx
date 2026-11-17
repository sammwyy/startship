import { useState } from "react";
import { Link } from "react-router-dom";
import { useProject } from "../../layouts/ProjectLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { remove, request } from "../../services/api";
import type { DomainRoute, WorkspaceDomain } from "../../services/types";
import { ProjectHeading } from "../../components/ProjectHeading";
import { RouteEditor } from "../../components/RouteEditor";
import { Icon } from "../../components/Icon";

export function DomainsView() {
  const { project, containers, routes, refresh } = useProject(),
    { user, can } = useSession(),
    ui = useUI();
  const [editor, setEditor] = useState<{
    route?: DomainRoute;
    domains: WorkspaceDomain[];
  }>();
  const [loading, setLoading] = useState(false);
  const editable = can("domains.manage", project);
  async function edit(route?: DomainRoute) {
    setLoading(true);
    try {
      const { domains } = await request<{ domains: WorkspaceDomain[] }>(
        `workspaces/${project.workspace_id}/domains`,
      );
      setEditor({ route, domains });
    } finally {
      setLoading(false);
    }
  }
  return (
    <>
      <ProjectHeading title="Domains" />
      <div className="section-head">
        <div>
          <h2>Domains</h2>
          <p className="small muted">
            Map public addresses to your containers.
          </p>
        </div>
        <button
          className="primary"
          disabled={!editable || !containers.length || loading || !!editor}
          onClick={() => ui.run(() => edit())}
        >
          <Icon name="plus" />
          {loading ? "Loading…" : "Connect domain"}
        </button>
      </div>
      {editor && (
        <>
          {!editor.domains.length && !user?.platform_admin && (
            <p className="note">
              Register a domain in{" "}
              <Link to={`/workspace/${project.workspace_id}/settings`}>
                Workspace settings
              </Link>{" "}
              first, or ask a workspace administrator.
            </p>
          )}
          <RouteEditor
            key={editor.route?.id || "new"}
            containers={containers}
            domains={editor.domains}
            route={editor.route}
            admin={user?.platform_admin}
            onCancel={() => setEditor(undefined)}
            onSave={async (data) => {
              await request(
                `routes${editor.route ? `/${editor.route.id}` : ""}`,
                { ...data, project_id: project.id },
                editor.route ? "PATCH" : "POST",
              );
              await refresh();
              setEditor(undefined);
              ui.notify("Route saved");
            }}
          />
        </>
      )}
      {!!routes.length && (
        <div className="panel domain-routes">
          <div className="domain-route-labels">
            <span>Public address</span>
            <span>Destination</span>
          </div>
          {routes.map((route) => {
            const target = containers.find(
              (c) => c.id === (route.container_id || route.app_id),
            );
            return (
              <div className="domain-route" key={route.id}>
                <div className="domain-route-address">
                  <span className="card-icon">
                    <Icon name="globe" />
                  </span>
                  <div>
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
                      <span className="muted">{route.path_prefix}</span>
                      <Icon name="arrow" />
                    </a>
                    <span className="small muted">
                      {route.strip_prefix
                        ? "Path prefix removed"
                        : "Original path forwarded"}
                    </span>
                  </div>
                </div>
                <div className="domain-route-target">
                  <Icon name="next" />
                  <span>
                    <Icon name="box" />
                    {target?.name || "Container unavailable"}
                    <strong>:{route.port || target?.port || "—"}</strong>
                  </span>
                </div>
                <div className="domain-route-actions">
                  <button
                    disabled={!editable || loading}
                    aria-label={`Edit ${route.host}${route.path_prefix}`}
                    onClick={() => ui.run(() => edit(route))}
                  >
                    Edit
                  </button>
                  <button
                    disabled={!editable}
                    aria-label={`Disconnect ${route.host}${route.path_prefix}`}
                    onClick={() =>
                      ui.dialog({
                        title: "Disconnect domain",
                        description: `Disconnect ${route.host}${route.path_prefix}? The container will keep running.`,
                        submit: "Disconnect",
                        onSubmit: async () => {
                          await remove(`routes/${route.id}`);
                          if (editor?.route?.id === route.id)
                            setEditor(undefined);
                          await refresh();
                        },
                      })
                    }
                  >
                    <Icon name="trash" />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}
      {!routes.length && !editor && (
        <div className="panel route-empty">
          <span className="card-icon">
            <Icon name="globe" />
          </span>
          <h3>
            {containers.length
              ? "Give your project an address"
              : "Deploy before connecting a domain"}
          </h3>
          <p className="muted">
            {containers.length
              ? "Connect a hostname and path. We’ll forward requests to the right container and port."
              : "Your first deployment detects the containers you can route traffic to."}
          </p>
          {!containers.length && (
            <Link
              className="text-link"
              to={`/project/${project.id}/deployments`}
            >
              Go to deployments <Icon name="next" />
            </Link>
          )}
        </div>
      )}
      {!!routes.length && (
        <p className="note">
          Routes with a longer matching path take priority. Use the same
          hostname with different paths to reach different containers.
        </p>
      )}
    </>
  );
}
