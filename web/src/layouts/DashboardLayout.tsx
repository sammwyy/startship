import {
  Suspense,
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import {
  Link,
  NavLink,
  Outlet,
  useLocation,
  useMatch,
  useNavigate,
} from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { queries } from "../services/queries";
import { request } from "../services/api";
import type { Project, Workspace } from "../services/types";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { projectSections, text } from "../lib/project";
import { Brand, Icon } from "../components/Icon";
import { Badge, Field, Loading, ThemeButton } from "../components/ui";
import { Popover } from "../components/Popover";
import { ProjectLoading } from "../components/ProjectLoading";
import { Skeleton } from "../components/Skeleton";
import { SystemUpdateLink } from "../components/SystemUpdateLink";
import { useLiveSubscription } from "../providers/LiveProvider";

interface DashboardContext {
  workspace?: Workspace;
  workspaces: Workspace[];
  workspacesPending: boolean;
  workspacesError?: Error | null;
  project?: Project;
}
const Context = createContext<DashboardContext>({
  workspaces: [],
  workspacesPending: false,
});
export const useDashboard = () => useContext(Context);

export function DashboardLayout() {
  const { user, can, logout } = useSession();
  const ui = useUI(),
    client = useQueryClient(),
    navigate = useNavigate(),
    location = useLocation();
  const projectMatch = useMatch("/project/:projectId/*"),
    workspaceMatch = useMatch("/workspace/:workspaceId/*");
  const {
    data: workspaces = [],
    isPending: workspacesPending,
    error: workspacesError,
  } = useQuery(queries.workspaces());
  const projectId = projectMatch?.params.projectId;
  const projectSection = projectMatch?.params["*"]?.split("/")[0] || "overview";
  const { data: project, isPending: projectPending } = useQuery({
    ...queries.project(projectId || ""),
    enabled: !!projectId,
  });
  useLiveSubscription(
    projectId ? { topic: "project", project_id: projectId } : null,
  );
  useLiveSubscription({ topic: "system_update" });
  useLiveSubscription({ topic: "workspaces" });
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem("startship-sidebar-collapsed") === "true",
  );
  const drawer = useRef<HTMLDialogElement>(null);
  const matchedWorkspace = workspaces.find(
    (w) =>
      w.id ===
      (projectId ? project?.workspace_id : workspaceMatch?.params.workspaceId),
  );
  const workspace = projectId
    ? matchedWorkspace
    : matchedWorkspace ||
      workspaces.find((w) => w.id === localStorage.getItem("workspace")) ||
      workspaces[0];
  const projectWorkspacePending =
    !!projectId && (projectPending || workspacesPending);
  useEffect(() => {
    if (workspace) localStorage.setItem("workspace", workspace.id);
  }, [workspace]);
  useEffect(() => {
    ui.setNavigation(false);
  }, [location.pathname]); // Close mobile navigation after a route change.
  useEffect(() => {
    if (ui.navigationOpen) drawer.current?.showModal();
    else drawer.current?.close();
  }, [ui.navigationOpen]);
  function createWorkspace() {
    ui.dialog({
      title: "Create workspace",
      description: "A home for your projects.",
      submit: "Create workspace",
      fields: <Field label="Workspace name" name="name" required />,
      onSubmit: async (form) => {
        const w = await request<Workspace>("workspaces", {
          name: text(form, "name"),
        });
        await client.invalidateQueries(queries.workspaces());
        navigate(`/workspace/${w.id}`);
        ui.notify("Workspace created");
      },
    });
  }
  const workspaceId = project?.workspace_id ?? workspace?.id;
  const url = workspaceId ? `/workspace/${workspaceId}` : "/workspaces";
  const accountName = user?.display_name || user?.username || "Account";
  const navLink = (to: string, name: string, shape: string, end = false) => (
    <NavLink key={to} to={to} end={end} aria-label={name} title={name}>
      <Icon name={shape} />
      <span>{name}</span>
    </NavLink>
  );
  const sidebar = (mobile: boolean) => (
    <aside
      className="sidebar"
      aria-label={mobile ? "Mobile dashboard sidebar" : "Dashboard sidebar"}
    >
      <div className="sidebar-top">
        <Link to={url} className="brand" aria-label="Startship">
          <Brand />
        </Link>
        <button
          className="icon-button sidebar-fold"
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          aria-expanded={!collapsed}
          onClick={() => {
            setCollapsed(!collapsed);
            localStorage.setItem(
              "startship-sidebar-collapsed",
              String(!collapsed),
            );
          }}
        >
          <Icon name="sidebar" />
        </button>
        <button
          className="icon-button mobile-only"
          aria-label="Close navigation"
          onClick={() => ui.setNavigation(false)}
        >
          <Icon name="close" />
        </button>
      </div>
      <Popover
        id={`workspace-menu-${mobile}`}
        className="sidebar-selector"
        label={
          projectWorkspacePending
            ? "Loading workspace"
            : `Workspace: ${workspace?.name || "Choose workspace"}`
        }
        trigger={
          <>
            <Icon name="layers" />
            <span className="selector-name">
              {projectWorkspacePending ? (
                <Skeleton width="5.5rem" />
              ) : (
                workspace?.name || "Choose workspace"
              )}
            </span>
            <Icon name="chevron" />
          </>
        }
      >
        <div className="menu-caption">Switch workspace</div>
        <div className="workspace-options">
          {workspaces.map((w) => (
            <Link
              key={w.id}
              to={`/workspace/${w.id}`}
              aria-current={w.id === workspace?.id ? "true" : undefined}
            >
              <span className="workspace-initial">
                {w.name[0]?.toUpperCase()}
              </span>
              <span>{w.name}</span>
              {w.id === workspace?.id && (
                <small className="current-workspace">Current</small>
              )}
            </Link>
          ))}
        </div>
        <div className="menu-divider" />
        {can("workspaces.create") && (
          <button onClick={createWorkspace}>
            <Icon name="plus" />
            Create workspace
          </button>
        )}
        <Link to="/workspaces">
          <Icon name="layers" />
          Manage workspaces
        </Link>
      </Popover>
      <div className="sidebar-body">
        <nav
          className="sidebar-nav"
          aria-label={
            projectMatch ? "Project navigation" : "Workspace navigation"
          }
        >
          {projectId ? (
            <>
              {navLink(url, "All projects", "back")}
              <div className="sidebar-project">
                {project ? (
                  <>
                    <strong>{project.name}</strong>
                    {project.sleeping && <Badge status="sleeping" />}
                    <Link
                      className="sidebar-source"
                      to={`/project/${projectId}/source`}
                      title={project.branch || "Connect source"}
                    >
                      <Icon name="branch" />
                      <span>
                        {project.git_url
                          ? project.branch || "main"
                          : "Connect source"}
                      </span>
                    </Link>
                  </>
                ) : (
                  <div role="status" aria-label="Loading project name">
                    <strong>
                      <span className="project-skeleton-line">
                        <Skeleton width="6rem" />
                      </span>
                    </strong>
                    <div
                      className="sidebar-source sidebar-source-placeholder"
                      aria-hidden="true"
                    >
                      <Icon name="branch" />
                      <Skeleton width="3.5rem" height="0.55rem" />
                    </div>
                  </div>
                )}
              </div>
              {projectSections.map(([section, label, icon]) =>
                navLink(`/project/${projectId}/${section}`, label, icon),
              )}
            </>
          ) : (
            <>
              <div className="sidebar-caption">Workspace</div>
              {navLink(url, "Projects", "grid", true)}
              {workspace && (
                <>
                  {navLink(`${url}/map`, "Map", "pipeline")}
                  {navLink(`${url}/services`, "Services", "database")}
                  {navLink(`${url}/apps`, "Apps", "box")}
                  {navLink(`${url}/volumes`, "Volumes", "hard-drive")}
                  {navLink(`${url}/backups`, "Backups", "history")}
                  {navLink(`${url}/domains`, "Domains", "globe")}
                  {can("firewall.view", workspace) &&
                    navLink(`${url}/firewall`, "Firewall", "shield")}
                  {navLink(
                    `${url}/certificates`,
                    "Certificates",
                    "shield-check",
                  )}
                  {navLink(`${url}/members`, "Members", "users")}
                  {navLink(`${url}/settings`, "Workspace settings", "settings")}
                </>
              )}
              <div className="sidebar-caption">Platform</div>
              {navLink("/roles", "Roles", "shield")}
              {can("users.manage") && navLink("/users", "Users", "user")}
              {can("audit.view") && navLink("/audit", "Audit log", "history")}
            </>
          )}
        </nav>
      </div>
      <div className="sidebar-bottom">
        <SystemUpdateLink />
        <Popover
          id={`account-menu-${mobile}`}
          className="sidebar-selector account-selector"
          label={`Account: ${user!.username}`}
          trigger={
            <>
              <span className="avatar">{accountName[0]?.toUpperCase()}</span>
              <span className="selector-name">{accountName}</span>
              <Icon name="chevron" />
            </>
          }
        >
          <div className="menu-caption">
            Signed in as <strong>{user!.username}</strong>
          </div>
          <Link to="/account">
            <Icon name="settings" />
            Account settings
          </Link>
          <ThemeButton />
          <div className="menu-divider" />
          <button onClick={() => ui.run(logout)}>
            <Icon name="logout" />
            Sign out
          </button>
        </Popover>
      </div>
    </aside>
  );
  return (
    <Context.Provider
      value={{
        workspace,
        workspaces,
        workspacesPending,
        workspacesError,
        project,
      }}
    >
      <div
        className={`dashboard sidebar-layout${collapsed ? " sidebar-collapsed" : ""}`}
      >
        <div id="desktop-sidebar">{sidebar(false)}</div>
        <dialog
          id="navigation-drawer"
          ref={drawer}
          aria-label="Navigation"
          onClose={() => ui.setNavigation(false)}
          onClick={(e) => {
            if (e.target === e.currentTarget) ui.setNavigation(false);
          }}
        >
          {ui.navigationOpen && sidebar(true)}
        </dialog>
        <div className="dashboard-body">
          <main className={projectId ? "main project-main" : "main"}>
            <div
              className={`content${/\/(source|environment|pipeline)$/.test(location.pathname) || /^\/system(?:\/|$)/.test(location.pathname) || /^\/workspace\/[^/]+\/settings$/.test(location.pathname) ? " content-compact" : ""}`}
            >
              <Suspense
                fallback={
                  projectId ? (
                    <ProjectLoading
                      section={projectSection}
                      projectName={project?.name}
                      showActivityTabs={
                        !project || can("activity.view", project)
                      }
                      canEditSettings={
                        !project || can("project.settings", project)
                      }
                    />
                  ) : (
                    <Loading
                      layout={
                        workspaceMatch && !workspaceMatch.params["*"]
                          ? "workspace"
                          : "page"
                      }
                    />
                  )
                }
              >
                <Outlet />
              </Suspense>
            </div>
          </main>
        </div>
      </div>
    </Context.Provider>
  );
}
