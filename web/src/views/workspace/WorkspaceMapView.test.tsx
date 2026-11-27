import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WorkspaceMapView } from "./WorkspaceMapView";

const state = vi.hoisted(() => ({
  empty: false,
  error: null as Error | null,
  pending: false,
  live: "connected",
  shared: false,
  isolated: false,
}));
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  const data = [
    {
      data: state.empty ? [] : [{ id: "p", name: "Website" }],
      error: state.error,
      isPending: state.pending,
    },
    { data: [{ project_id: "p", active_deployment: "r", status: "running" }] },
    { data: [{ project_id: "p", status: "running", created: "2026-01-01" }] },
  ];
  const empty: never[] = [];
  const sharedProjects = [
    { id: "p", name: "Website" },
    { id: "q", name: "Worker" },
  ];
  const isolatedProjects = [{ id: "alone", name: "Isolated" }];
  return {
    ...actual,
    useQueries: () => [
      {
        ...data[0],
        data: state.empty
          ? empty
          : state.isolated
            ? isolatedProjects
            : state.shared
              ? sharedProjects
              : data[0].data,
        error: state.error,
        isPending: state.pending,
      },
      data[1],
      data[2],
    ],
  };
});
vi.mock("../../layouts/DashboardLayout", () => ({
  useDashboard: () => ({ workspace: { name: "Studio" } }),
}));
vi.mock("../../providers/UIProvider", () => ({
  useUI: () => ({ openNavigation: vi.fn() }),
}));
vi.mock("../../providers/LiveProvider", () => ({
  useLiveProjects: vi.fn(),
  useLiveSubscription: () => null,
  useLiveStatus: () => state.live,
}));
vi.mock("../../services/services", () => {
  const hooks = {
    useServices: () => ({
      data: {
        services: state.empty
          ? []
          : [
              {
                id: "s",
                name: "Database",
                engine: "postgresql",
                version: "17",
                status: "running",
                volume_id: "v",
                attachments: [
                  { project_id: "p", alias: "db" },
                  { project_id: "q", alias: "shared-db" },
                ],
              },
              {
                id: "other",
                name: "Unattached",
                engine: "valkey",
                version: "8",
                status: "stopped",
                attachments: [],
              },
            ],
      },
    }),
    useVolumes: () => ({
      data: {
        volumes: state.empty
          ? []
          : [
              {
                id: "v",
                name: "Database data",
                size_bytes: 1024,
                size_updated: "2026-01-01",
              },
              {
                id: "mount",
                name: "Uploads",
                project_id: "p",
                mount_path: "/uploads",
                size_bytes: 2048,
                size_updated: "2026-01-01",
              },
              { id: "loose", name: "Loose volume", size_bytes: 0 },
            ],
      },
    }),
  };
  const services = hooks.useServices(),
    volumes = hooks.useVolumes();
  const emptyServices = { data: { services: [] } },
    emptyVolumes = { data: { volumes: [] } };
  return {
    useServices: () => (state.empty ? emptyServices : services),
    useVolumes: () => (state.empty ? emptyVolumes : volumes),
  };
});

beforeEach(() => {
  state.empty = false;
  state.error = null;
  state.pending = false;
  state.live = "connected";
  state.shared = false;
  state.isolated = false;
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});
afterEach(() => vi.restoreAllMocks());
function setup() {
  render(
    <MemoryRouter initialEntries={["/workspace/w/map"]}>
      <Routes>
        <Route
          path="/workspace/:workspaceId/map"
          element={<WorkspaceMapView />}
        />
      </Routes>
    </MemoryRouter>,
  );
  return userEvent.setup();
}
describe("workspace map view", () => {
  it("shows node statuses as text without a connected-updates indicator", () => {
    setup();
    const database = screen.getByRole("link", { name: /^Database postgresql/ });
    expect(within(database).getByText("· running")).toBeVisible();
    const uploads = screen.getByRole("link", { name: /Uploads/ });
    expect(within(uploads).getByText("· running")).toBeVisible();
    expect(screen.queryByText("Live updates")).not.toBeInTheDocument();
  });
  it("shows when map updates are disconnected", () => {
    state.live = "disconnected";
    setup();
    expect(screen.getByText("Updates disconnected")).toBeVisible();
  });
  it("includes connection aliases and mounts in card tooltips and accessible names", () => {
    setup();
    const project = screen.getByRole("link", { name: /Website/ });
    expect(project).toHaveAttribute(
      "title",
      expect.stringContaining("Connections: db, /uploads"),
    );
    expect(project).toHaveAccessibleName(
      expect.stringContaining("Connections: db, /uploads"),
    );
    expect(
      screen.getByRole("link", { name: /^Database postgresql/ }),
    ).toHaveAttribute("title", expect.stringContaining("Connections: db"));
    expect(
      screen.getByRole("link", { name: /Unattached/ }),
    ).not.toHaveAttribute("title", expect.stringContaining("Connections:"));
  });
  it("puts all volumes in Volumes and reserves an invisible service lane for mounts", () => {
    setup();
    const services = within(screen.getByRole("region", { name: "services" }));
    expect(
      services.getByRole("link", { name: /^Database postgresql/ }),
    ).toBeInTheDocument();
    expect(
      services.queryByRole("link", { name: /Uploads/ }),
    ).not.toBeInTheDocument();
    const volumes = within(screen.getByRole("region", { name: "volumes" }));
    expect(volumes.getByRole("link", { name: /Uploads/ })).toHaveAttribute(
      "title",
      expect.stringContaining("/uploads · 2.0 KB"),
    );
    expect(
      volumes.getByRole("link", { name: /Database data/ }),
    ).toBeInTheDocument();
    expect(document.querySelector('[data-map-key="lane:mount"]')).toHaveClass(
      "map-lane",
    );
    expect(document.querySelectorAll(".map-edge-label")).toHaveLength(2);
    expect(
      [...document.querySelectorAll(".map-edge-label title")].map(
        (title) => title.textContent,
      ),
    ).toContain("/uploads");
  });
  it("renders a compact per-project tree with aliases, nested data volumes and orphans on narrow screens", async () => {
    vi.spyOn(window, "getComputedStyle").mockReturnValue({
      getPropertyValue: () => "1",
    } as unknown as CSSStyleDeclaration);
    const user = setup();
    const project = within(screen.getByRole("region", { name: "Website" }));
    const database = project.getByRole("link", { name: /^db → Database/ });
    expect(database).toHaveAttribute("href", "/workspace/w/services/s");
    expect(
      within(database.closest("li")!).getByRole("link", {
        name: /Database data/,
      }),
    ).toBeInTheDocument();
    expect(
      project.getByRole("link", { name: /uploads → Uploads/ }),
    ).toHaveAttribute("href", "/workspace/w/volumes");
    const orphans = within(
      screen.getByRole("region", { name: "Not connected" }),
    );
    expect(
      orphans.getByRole("link", { name: /Unattached/ }),
    ).toBeInTheDocument();
    expect(
      orphans.getByRole("link", { name: /Loose volume/ }),
    ).toBeInTheDocument();
    expect(
      orphans.queryByRole("link", { name: /Database data/ }),
    ).not.toBeInTheDocument();
    expect(document.querySelector(".map-edges")).not.toBeInTheDocument();
    await user.tab();
    await user.tab();
    await user.tab();
    expect(database).toHaveFocus();
  });
  it("repeats shared services and their data under each owning project", () => {
    state.shared = true;
    vi.spyOn(window, "getComputedStyle").mockReturnValue({
      getPropertyValue: () => "1",
    } as unknown as CSSStyleDeclaration);
    setup();
    const website = within(screen.getByRole("region", { name: "Website" }));
    const worker = within(screen.getByRole("region", { name: "Worker" }));
    expect(
      website.getByRole("link", { name: /^db → Database/ }),
    ).toBeInTheDocument();
    expect(
      worker.getByRole("link", { name: /^shared-db → Database/ }),
    ).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: /Database data/ })).toHaveLength(
      2,
    );
  });
  it("keeps disconnected project trees compact and nests orphan service data only once", () => {
    state.isolated = true;
    vi.spyOn(window, "getComputedStyle").mockReturnValue({
      getPropertyValue: () => "1",
    } as unknown as CSSStyleDeclaration);
    setup();
    expect(
      within(screen.getByRole("region", { name: "Isolated" })).getByText(
        "No connections yet",
      ),
    ).toBeInTheDocument();
    const orphans = within(
      screen.getByRole("region", { name: "Not connected" }),
    );
    const database = orphans.getByRole("link", {
      name: /^Database postgresql/,
    });
    expect(
      within(database.closest("li")!).getByRole("link", {
        name: /Database data/,
      }),
    ).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: /Database data/ })).toHaveLength(
      1,
    );
  });
  it("renders resource links and traces connected nodes on keyboard focus", async () => {
    const user = setup();
    const project = screen.getByRole("link", { name: /Website/ });
    expect(project).toHaveAttribute("href", "/project/p");
    expect(
      screen.getByRole("link", { name: /^Database postgresql/ }),
    ).toHaveAttribute("href", "/workspace/w/services/s");
    expect(screen.getByRole("link", { name: /Database data/ })).toHaveAttribute(
      "href",
      "/workspace/w/volumes",
    );
    expect(screen.getByText("1.0 KB")).toBeInTheDocument();
    await user.tab();
    await user.tab();
    expect(project).toHaveFocus();
    expect(screen.getByRole("link", { name: /Unattached/ })).toHaveClass(
      "is-dimmed",
    );
    expect(screen.getByRole("link", { name: /Database data/ })).not.toHaveClass(
      "is-dimmed",
    );
    await user.unhover(project);
    expect(screen.getByRole("link", { name: /Unattached/ })).toHaveClass(
      "is-dimmed",
    );
  });
  it("shows an empty workspace", () => {
    state.empty = true;
    setup();
    expect(screen.getByText("Your workspace, connected")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
  it("shows failures without presenting a failed request as an empty workspace", () => {
    state.empty = true;
    state.error = new Error("Access denied");
    setup();
    expect(screen.getByRole("alert")).toHaveTextContent("Access denied");
    expect(
      screen.queryByText("Your workspace, connected"),
    ).not.toBeInTheDocument();
  });
  it("shows loading and disconnected live states honestly", () => {
    state.pending = true;
    const { unmount } = render(
      <MemoryRouter>
        <WorkspaceMapView />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Loading");
    unmount();
    state.pending = false;
    state.live = "disconnected";
    setup();
    expect(screen.getByText("Updates disconnected")).toBeInTheDocument();
  });
});
