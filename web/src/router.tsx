import { Suspense } from "react";
import {
  createHashRouter,
  Navigate,
  Outlet,
  useParams,
  useRouteError,
} from "react-router-dom";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useSession } from "./providers/SessionProvider";
import { useDashboard, DashboardLayout } from "./layouts/DashboardLayout";
import { ProjectLayout } from "./layouts/ProjectLayout";
import { AuthView } from "./views/AuthView";
import { ErrorMessage, Loading } from "./components/ui";
import { request } from "./services/api";
import type { Container } from "./services/types";

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
    <Suspense fallback={<Loading />}>
      <Outlet />
    </Suspense>
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
function LegacyContainer() {
  const { containerId, section } = useParams();
  const { data } = useSuspenseQuery({
    queryKey: ["container", containerId],
    queryFn: () => request<Container>(`containers/${containerId}`),
  });
  return (
    <Navigate
      replace
      to={`/project/${data.project_id}/${section === "settings" ? "settings" : section === "domains" ? "domains" : `logs/${data.id}`}`}
    />
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
            lazy: async () => ({
              Component: (await import("./views/SystemView")).SystemView,
            }),
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
          { path: "app/:containerId/:section?", element: <LegacyContainer /> },
          {
            path: "project/:projectId",
            element: <ProjectLayout />,
            children: [
              { index: true, element: <Navigate to="deployments" replace /> },
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
                path: "settings",
                lazy: async () => ({
                  Component: (await import("./views/project/SettingsView"))
                    .SettingsView,
                }),
              },
              {
                path: "traffic",
                lazy: async () => ({
                  Component: (await import("./views/project/TrafficView"))
                    .TrafficView,
                }),
              },
              { path: "git", element: <Navigate to="../source" replace /> },
              { path: "webhook", element: <Navigate to="../source" replace /> },
              { path: "*", element: <Navigate to="../deployments" replace /> },
            ],
          },
          { path: "*", element: <Navigate to="/workspaces" replace /> },
        ],
      },
    ],
  },
]);
