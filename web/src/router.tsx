import { Suspense } from "react";
import {
  createHashRouter,
  Navigate,
  Outlet,
  useRouteError,
} from "react-router-dom";
import { useSession } from "./providers/SessionProvider";
import { useDashboard, DashboardLayout } from "./layouts/DashboardLayout";
import { ProjectLayout } from "./layouts/ProjectLayout";
import { SystemLayout } from "./layouts/SystemLayout";
import { AuthView } from "./views/AuthView";
import { LiveProvider } from "./providers/LiveProvider";
import { ErrorMessage, Loading } from "./components/ui";

function SessionGate() {
  const session = useSession();
  if (session.loading) return <Loading />;
  if (session.error) return <ErrorMessage error={session.error} />;
  if (
    !session.user ||
    session.user.must_change_password ||
    session.setupRequired
  )
    return <AuthView />;
  return (
    <LiveProvider>
      <Suspense fallback={<Loading />}>
        <Outlet />
      </Suspense>
    </LiveProvider>
  );
}
function Home() {
  const { workspace } = useDashboard();
  return (
    <Navigate
      to={workspace ? `/workspace/${workspace.id}` : "/workspaces"}
      replace
    />
  );
}
function RouteError() {
  const error = useRouteError();
  return (
    <div className="error-page">
      <h1>Something went wrong.</h1>
      <ErrorMessage error={error} />
      <a className="button" href="#workspaces">
        Back to workspaces
      </a>
      <button onClick={() => location.reload()}>Try again</button>
    </div>
  );
}

export const router = createHashRouter([
  {
    element: <SessionGate />,
    errorElement: <RouteError />,
    children: [
      {
        element: <DashboardLayout />,
        children: [
          { index: true, element: <Home /> },
          { path: "login", element: <Home /> },
          {
            path: "system",
            element: <SystemLayout />,
            children: [
              {
                path: "network-firewall",
                lazy: async () => ({
                  Component: (await import("./views/NetworkFirewallView"))
                    .NetworkFirewallView,
                }),
              },
              {
                index: true,
                lazy: async () => ({
                  Component: (await import("./views/NodeSettingsView"))
                    .NodeSettingsView,
                }),
              },
              {
                path: "updates",
                lazy: async () => ({
                  Component: (await import("./views/SystemView")).SystemView,
                }),
              },
            ],
          },
          {
            path: "workspaces",
            lazy: async () => ({
              Component: (await import("./views/WorkspacesView"))
                .WorkspacesView,
            }),
          },
          {
            path: "workspace/:workspaceId",
            lazy: async () => ({
              Component: (await import("./views/WorkspaceView")).WorkspaceView,
            }),
          },
          {
            path: "workspace/:workspaceId/map",
            lazy: async () => ({
              Component: (await import("./views/workspace/WorkspaceMapView"))
                .WorkspaceMapView,
            }),
          },
          {
            path: "workspace/:workspaceId/domains",
            lazy: async () => ({
              Component: (await import("./views/WorkspaceDomainsView"))
                .WorkspaceDomainsView,
            }),
          },
          {
            path: "workspace/:workspaceId/apps/marketplace/:entryId?",
            lazy: async () => ({
              Component: (await import("./views/workspace/RecipeAppsView"))
                .AppsMarketplaceView,
            }),
          },
          {
            path: "workspace/:workspaceId/apps/:appId?",
            lazy: async () => ({
              Component: (await import("./views/workspace/RecipeAppsView"))
                .RecipeAppsView,
            }),
          },
          {
            path: "workspace/:workspaceId/services",
            lazy: async () => ({
              Component: (
                await import("./views/workspace/WorkspaceServicesView")
              ).WorkspaceServicesView,
            }),
          },
          {
            path: "workspace/:workspaceId/services/:serviceId",
            lazy: async () => ({
              Component: (await import("./views/workspace/ServiceDetailView"))
                .ServiceDetailView,
            }),
          },
          {
            path: "workspace/:workspaceId/volumes",
            lazy: async () => ({
              Component: (
                await import("./views/workspace/WorkspaceVolumesView")
              ).WorkspaceVolumesView,
            }),
          },
          {
            path: "workspace/:workspaceId/backups",
            lazy: async () => ({
              Component: (
                await import("./views/workspace/WorkspaceBackupsView")
              ).WorkspaceBackupsView,
            }),
          },
          {
            path: "workspace/:workspaceId/certificates",
            lazy: async () => ({
              Component: (
                await import("./views/workspace/WorkspaceCertificatesView")
              ).WorkspaceCertificatesView,
            }),
          },
          {
            path: "workspace/:workspaceId/firewall/:section?",
            lazy: async () => ({
              Component: (await import("./views/workspace/FirewallView"))
                .FirewallView,
            }),
          },
          {
            path: "workspace/:workspaceId/settings",
            lazy: async () => ({
              Component: (await import("./views/WorkspaceSettingsView"))
                .WorkspaceSettingsView,
            }),
          },
          {
            path: "workspace/:workspaceId/members",
            lazy: async () => ({
              Component: (await import("./views/access/MembersView"))
                .MembersView,
            }),
          },
          {
            path: "roles/:roleId?",
            lazy: async () => ({
              Component: (await import("./views/access/RolesView")).RolesView,
            }),
          },
          {
            path: "users",
            lazy: async () => ({
              Component: (await import("./views/access/UsersView")).UsersView,
            }),
          },
          {
            path: "audit",
            lazy: async () => ({
              Component: (await import("./views/access/AuditView")).AuditView,
            }),
          },
          {
            path: "account/:section?",
            lazy: async () => ({
              Component: (await import("./views/access/AccountView"))
                .AccountView,
            }),
          },
          {
            path: "workspace/:workspaceId/activity",
            lazy: async () => ({
              Component: (await import("./views/access/ActivityView"))
                .ActivityView,
            }),
          },
          {
            path: "project/:projectId",
            element: <ProjectLayout />,
            children: [
              {
                path: "activity",
                lazy: async () => ({
                  Component: (await import("./views/access/ActivityView"))
                    .ActivityView,
                }),
              },
              { index: true, element: <Navigate to="overview" replace /> },
              {
                path: "overview",
                lazy: async () => ({
                  Component: (await import("./views/project/OverviewView"))
                    .OverviewView,
                }),
              },
              {
                path: "deployments",
                lazy: async () => ({
                  Component: (await import("./views/project/DeploymentsView"))
                    .DeploymentsView,
                }),
              },
              {
                path: "logs/:containerId?",
                lazy: async () => ({
                  Component: (await import("./views/project/LogsView"))
                    .LogsView,
                }),
              },
              {
                path: "containers",
                lazy: async () => ({
                  Component: (await import("./views/project/ContainersView"))
                    .ContainersView,
                }),
              },
              {
                path: "connections",
                lazy: async () => ({
                  Component: (await import("./views/project/ConnectionsView"))
                    .ConnectionsView,
                }),
              },
              {
                path: "volumes",
                lazy: async () => ({
                  Component: (
                    await import("./views/project/ProjectVolumesView")
                  ).ProjectVolumesView,
                }),
              },
              {
                path: "domains",
                lazy: async () => ({
                  Component: (await import("./views/project/DomainsView"))
                    .DomainsView,
                }),
              },
              {
                path: "source",
                lazy: async () => ({
                  Component: (await import("./views/project/SourceView"))
                    .SourceView,
                }),
              },
              {
                path: "environment",
                lazy: async () => ({
                  Component: (await import("./views/project/EnvironmentView"))
                    .EnvironmentView,
                }),
              },
              {
                path: "pipeline",
                lazy: async () => ({
                  Component: (await import("./views/project/PipelineView"))
                    .PipelineView,
                }),
              },
              {
                path: "settings",
                element: <Navigate to="../overview" replace />,
              },
              {
                path: "traffic",
                lazy: async () => ({
                  Component: (await import("./views/project/TrafficView"))
                    .TrafficView,
                }),
              },
              {
                path: "uptime",
                lazy: async () => ({
                  Component: (await import("./views/project/UptimeView"))
                    .UptimeView,
                }),
              },
              { path: "git", element: <Navigate to="../source" replace /> },
              { path: "webhook", element: <Navigate to="../source" replace /> },
              { path: "*", element: <Navigate to="../overview" replace /> },
            ],
          },
          { path: "*", element: <Navigate to="/workspaces" replace /> },
        ],
      },
    ],
  },
]);
