import { createContext, useContext, useEffect, useRef, useState } from "react";
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
import { Field, ThemeButton } from "../components/ui";
import { Popover } from "../components/Popover";
import { SystemUpdateLink } from "../components/SystemUpdateLink";
import { useLiveStatus, useLiveSubscription } from "../providers/LiveProvider";

interface DashboardContext {
  workspace?: Workspace;
  workspaces: Workspace[];
  project?: Project;
}
const Context = createContext<DashboardContext>({ workspaces: [] });
export const useDashboard = () => useContext(Context);

export function DashboardLayout() {
  const { user, can, logout, updateAccount } = useSession();
  const ui = useUI(),
    client = useQueryClient(),
    navigate = useNavigate(),
    location = useLocation();
  const projectMatch = useMatch("/project/:projectId/*"),
    workspaceMatch = useMatch("/workspace/:workspaceId/*");
  const { data: workspaces = [] } = useQuery(queries.workspaces());
  const { data: project } = useQuery({
    ...queries.project(projectMatch?.params.projectId || ""),
    enabled: !!projectMatch,
  });
  useLiveSubscription(
    projectMatch?.params.projectId
      ? { topic: "project", project_id: projectMatch.params.projectId }
      : null,
  );
  useLiveSubscription({ topic: "system_update" });
  const liveStatus = useLiveStatus();
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem("startship-sidebar-collapsed") === "true",
  );
  const drawer = useRef<HTMLDialogElement>(null);
  const workspace =
    workspaces.find(
      (w) =>
        w.id ===
        (projectMatch
          ? project?.workspace_id
          : workspaceMatch?.params.workspaceId),
    ) ||
    workspaces.find((w) => w.id === localStorage.getItem("workspace")) ||
    workspaces[0];
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
  function accountSettings() {
    ui.dialog({
      title: "Account settings",
      description: "Confirm your current password to save changes.",
      submit: "Save account",
      fields: (
        <>
          <Field
            label="Username"
            name="username"
            defaultValue={user!.username}
            required
            maxLength={100}
            autoComplete="username"
          />
          <div className="split">
            <Field
              label="New password (optional)"
              name="new_password"
              type="password"
              minLength={8}
              maxLength={72}
              autoComplete="new-password"
            />
            <Field
              label="Confirm new password"
              name="confirm_password"
              type="password"
              autoComplete="new-password"
            />
          </div>
          <Field
            label="Current password"
            name="current_password"
            type="password"
            required
            autoComplete="current-password"
          />
          <p className="note">
            Saving signs out your other sessions; this session stays signed in.
          </p>
        </>
      ),
      onSubmit: async (form) => {
        const password = text(form, "new_password");
        if (password !== text(form, "confirm_password"))
          throw new Error("New passwords do not match");
        await updateAccount({
          username: text(form, "username"),
          current_password: text(form, "current_password"),
          ...(password ? { new_password: password } : {}),
        });
        ui.notify("Account updated");
      },
    });
  }
  const url = workspace ? `/workspace/${workspace.id}` : "/workspaces";
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
        label={`Workspace: ${workspace?.name || "Choose workspace"}`}
        trigger={
          <>
            <Icon name="layers" />
            <span className="selector-name">
              {workspace?.name || "Choose workspace"}
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
                <span className="current-dot" aria-label="Current workspace" />
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
          {projectMatch && project ? (
            <>
              {navLink(url, "All projects", "back")}
              <div className="sidebar-project">
                <strong>{project.name}</strong>
                <Link
                  className="sidebar-source"
                  to={`/project/${project.id}/source`}
                  title={project.branch || "Connect source"}
                >
                  <Icon name="branch" />
                  <span>
                    {project.git_url
                      ? project.branch || "main"
                      : "Connect source"}
                  </span>
                </Link>
              </div>
              {projectSections.map(([section, label, icon]) =>
                navLink(`/project/${project.id}/${section}`, label, icon),
              )}
            </>
          ) : (
            <>
              <div className="sidebar-caption">Workspace</div>
              {navLink(url, "Projects", "grid", true)}
              {workspace && (
                <>
                  {navLink(`${url}/domains`, "Domains", "globe")}
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
              <span className="avatar">{user!.username[0]?.toUpperCase()}</span>
              <span className="selector-name">{user!.username}</span>
              <Icon name="chevron" />
            </>
          }
        >
          <div className="menu-caption">
            Signed in as <strong>{user!.username}</strong>
          </div>
          <button onClick={accountSettings}>
            <Icon name="settings" />
            Account settings
          </button>
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
    <Context.Provider value={{ workspace, workspaces, project }}>
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
          <main className="main">
            <div
              className={`content${/\/(source|environment|pipeline)$/.test(location.pathname) || /^\/system(?:\/|$)/.test(location.pathname) || /^\/workspace\/[^/]+\/settings$/.test(location.pathname) ? " content-compact" : ""}`}
            >
              <Outlet />
            </div>
            <footer className="app-footer">
              <span>
                Startship <span className="footer-separator">/</span> Deployment
                platform
              </span>
              <span className={`live-connection ${liveStatus}`} role="status">
                <span />
                {liveStatus === "connected"
                  ? "Live updates connected"
                  : liveStatus === "disconnected"
                    ? "Live updates disconnected"
                    : "Connecting live updates…"}
              </span>
            </footer>
          </main>
        </div>
      </div>
    </Context.Provider>
  );
}
