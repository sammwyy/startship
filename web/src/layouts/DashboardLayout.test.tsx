import {
  QueryClient,
  QueryClientProvider,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { act, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { request } from "../services/api";
import { queries } from "../services/queries";
import { DashboardLayout } from "./DashboardLayout";
import { ProjectLayout } from "./ProjectLayout";
import { WorkspacesView } from "../views/WorkspacesView";
import type { Project, Workspace } from "../services/types";

vi.mock("../services/api", () => ({ request: vi.fn() }));

vi.mock("../providers/SessionProvider", () => ({
  useSession: () => ({ user: { username: "member" }, can: () => false }),
}));
vi.mock("../providers/UIProvider", () => ({
  useUI: () => ({ navigationOpen: false, setNavigation: vi.fn() }),
}));
vi.mock("../providers/LiveProvider", () => ({
  useLiveStatus: () => "connected",
  useLiveSubscription: vi.fn(),
}));

const dialogClose = HTMLDialogElement.prototype.close;
afterEach(() => {
  HTMLDialogElement.prototype.close = dialogClose;
});

it("shows workspaces skeletons instead of an empty state until the list resolves", async () => {
  HTMLDialogElement.prototype.close = vi.fn();
  let resolve: (workspaces: Workspace[]) => void = () => {};
  vi.mocked(request).mockImplementation(
    (path) =>
      (path === "workspaces"
        ? new Promise<Workspace[]>((done) => (resolve = done))
        : Promise.resolve({})) as ReturnType<typeof request>,
  );
  const client = new QueryClient();
  const { container } = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspaces"]}>
        <Routes>
          <Route element={<DashboardLayout />}>
            <Route path="/workspaces" element={<WorkspacesView />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(
    screen.getByRole("status", { name: "Loading workspaces…" }),
  ).toBeVisible();
  expect(container.querySelectorAll(".skeleton-card")).toHaveLength(3);
  expect(screen.queryByText("No workspaces yet")).not.toBeInTheDocument();
  await act(async () => resolve([]));
  expect(await screen.findByText("No workspaces yet")).toBeVisible();
});

it("keeps the dashboard sidebar visible when outlet data suspends", async () => {
  HTMLDialogElement.prototype.close = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  });
  client.setQueryData(queries.workspaces().queryKey, []);
  client.setQueryData(queries.updates().queryKey, {
    current: "0.1.0",
    commit: "abc",
    development: false,
    self_update: false,
    update_available: false,
  });
  let finish: (value: string) => void = () => {};
  const pending = new Promise<string>((resolve) => (finish = resolve));
  function WorkspaceContent() {
    const { data } = useSuspenseQuery({
      queryKey: ["delayed"],
      queryFn: () => pending,
    });
    return <h1>{data}</h1>;
  }
  const { container } = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspace/studio"]}>
        <Routes>
          <Route element={<DashboardLayout />}>
            <Route
              path="/workspace/:workspaceId"
              element={<WorkspaceContent />}
            />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(
    screen.getByRole("complementary", { name: "Dashboard sidebar" }),
  ).toBeVisible();
  expect(screen.getByRole("status", { name: "Loading…" })).toBeVisible();
  expect(container.querySelectorAll(".skeleton-stat")).toHaveLength(3);
  await act(async () => finish("Projects"));
  expect(
    await screen.findByRole("heading", { name: "Projects" }),
  ).toBeVisible();
});

it.each([
  ["overview", "Overview", true],
  ["activity", undefined, true],
  ["deployments", "Deployments", false],
  ["logs/container-1", "Logs", false],
] as const)(
  "keeps project navigation in place while %s is loading",
  async (section, activeLink, showsTabs) => {
    HTMLDialogElement.prototype.close = vi.fn();
    const project: Project = {
      id: "api",
      workspace_id: "studio",
      name: "Production API",
      permissions: [],
    };
    let resolve: (value: Project) => void = () => {};
    const pending = new Promise<Project>((done) => (resolve = done));
    vi.mocked(request).mockImplementation(
      (path) =>
        (path === "projects/api" ? pending : Promise.resolve([])) as ReturnType<
          typeof request
        >,
    );
    const client = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity } },
    });
    client.setQueryData(queries.workspaces().queryKey, [
      { id: "other", name: "Other workspace" },
      { id: "studio", name: "Studio" },
    ]);
    const { container } = render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[`/project/api/${section}`]}>
          <Routes>
            <Route element={<DashboardLayout />}>
              <Route path="/project/:projectId" element={<ProjectLayout />}>
                <Route path="*" element={<div>Project ready</div>} />
              </Route>
            </Route>
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    const nav = screen.getByRole("navigation", { name: "Project navigation" });
    const overview = within(nav).getByRole("link", { name: "Overview" });
    expect(overview).toHaveAttribute("href", "/project/api/overview");
    if (activeLink)
      expect(
        within(nav).getByRole("link", { name: activeLink }),
      ).toHaveAttribute("aria-current", "page");
    expect(within(nav).queryByRole("link", { name: "Roles" })).toBeNull();
    expect(
      within(nav).getByRole("status", { name: "Loading project name" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Loading workspace" }),
    ).toBeVisible();
    expect(!!container.querySelector(".project-skeleton-tabs")).toBe(showsTabs);
    expect(screen.getByRole("main")).toHaveClass("project-main");
    await act(async () => resolve(project));
    expect(await screen.findByText("Project ready")).toBeVisible();
    expect(within(nav).getByText("Production API")).toBeVisible();
    expect(
      within(nav).getByRole("link", { name: "All projects" }),
    ).toHaveAttribute("href", "/workspace/studio");
    expect(within(nav).getByRole("link", { name: "Overview" })).toBe(overview);
  },
);

it("omits the live connection footer but shows workspace selection and update availability as text", () => {
  HTMLDialogElement.prototype.close = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  });
  client.setQueryData(queries.workspaces().queryKey, [
    { id: "studio", name: "Studio" },
    { id: "team", name: "Team" },
  ]);
  client.setQueryData(queries.updates().queryKey, {
    current: "0.1.0",
    commit: "abc",
    development: false,
    self_update: true,
    update_available: true,
  });

  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspace/studio"]}>
        <DashboardLayout />
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(screen.queryByText("Live updates connected")).not.toBeInTheDocument();
  expect(screen.getByText("Current").closest("a")).toHaveAttribute(
    "aria-current",
    "true",
  );
  expect(screen.getByText("System: update available")).toBeVisible();
});
