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
  return {
    ...actual,
    useQueries: () => [
      {
        ...data[0],
        data: state.empty ? empty : data[0].data,
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
                attachments: [{ project_id: "p", alias: "db" }],
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
  it("puts project mounts beside services and service data in storage", () => {
    setup();
    const connections = within(
      screen.getByRole("region", { name: "connections" }),
    );
    expect(
      connections.getByRole("link", { name: /Uploads/ }),
    ).toBeInTheDocument();
    expect(connections.getByText("/uploads · 2.0 KB")).toBeInTheDocument();
    expect(
      connections.getByRole("link", { name: /Uploads/ }).parentElement,
    ).toHaveStyle("--map-card-columns: 3");
    expect(
      screen
        .getByRole("region", { name: "projects" })
        .querySelector(".map-node-list"),
    ).toHaveStyle("--map-card-columns: 1");
    expect(connections.getByRole("link", { name: /Uploads/ })).toHaveAttribute(
      "title",
      expect.stringContaining("/uploads · 2.0 KB"),
    );
    expect(
      connections.getByRole("link", { name: /^Database postgresql/ }),
    ).toBeInTheDocument();
    const storage = within(screen.getByRole("region", { name: "storage" }));
    expect(
      storage.getByRole("link", { name: /Database data/ }),
    ).toBeInTheDocument();
    expect(
      storage.queryByRole("link", { name: /Uploads/ }),
    ).not.toBeInTheDocument();
  });
  it("anchors vertical edges at the card bottom and the next card top", () => {
    vi.spyOn(window, "getComputedStyle").mockReturnValue({
      getPropertyValue: () => "1",
    } as unknown as CSSStyleDeclaration);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        if (this.classList.contains("map-board"))
          return {
            left: 0,
            right: 390,
            top: 0,
            bottom: 800,
            width: 390,
            height: 800,
            x: 0,
            y: 0,
            toJSON() {},
          };
        const top = this.dataset.mapKey === "projects:p" ? 100 : 300;
        return {
          left: 0,
          right: 100,
          top,
          bottom: top + 72,
          width: 100,
          height: 72,
          x: 0,
          y: top,
          toJSON() {},
        };
      },
    );
    setup();
    expect(document.querySelector(".map-edges path")).toHaveAttribute(
      "d",
      "M50,172 C50,236 50,236 50,300",
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
