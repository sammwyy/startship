import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkspaceView } from "./WorkspaceView";

const state = vi.hoisted(() => ({ sleeping: false }));
beforeEach(() => {
  state.sleeping = false;
});
afterEach(cleanup);

vi.mock("@tanstack/react-query", async (original) => ({
  ...(await original<typeof import("@tanstack/react-query")>()),
  useSuspenseQueries: () => [
    {
      data: [
        {
          id: "one",
          name: "Storefront",
          workspace_id: "w1",
          sleeping: state.sleeping,
        },
        { id: "two", name: "Documentation", workspace_id: "w1" },
      ],
    },
    {
      data: [
        {
          id: "c1",
          project_id: "one",
          status: "running",
          active_deployment: "release-1",
        },
      ],
    },
    { data: [] },
  ],
}));
vi.mock("../layouts/DashboardLayout", () => ({
  useDashboard: () => ({ workspace: { id: "w1", name: "Team" } }),
}));
vi.mock("../providers/SessionProvider", () => ({
  useSession: () => ({ can: () => true }),
}));
vi.mock("../providers/UIProvider", () => ({
  useUI: () => ({ dialog: vi.fn(), notify: vi.fn() }),
}));

describe("project directory", () => {
  it("excludes sleeping projects from the running container count", () => {
    state.sleeping = true;
    const client = new QueryClient();
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/workspace/w1"]}>
          <Routes>
            <Route path="/workspace/:workspaceId" element={<WorkspaceView />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(
      within(
        screen.getByText("Running containers").closest(".stat") as HTMLElement,
      ).getByText("0"),
    ).toBeTruthy();
    client.clear();
  });
  it("has one page heading and filters the entire project list through the standalone search", () => {
    const client = new QueryClient();
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/workspace/w1"]}>
          <Routes>
            <Route path="/workspace/:workspaceId" element={<WorkspaceView />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.getAllByRole("heading", { name: /^Projects/ })).toHaveLength(
      1,
    );
    const search = screen.getByRole("searchbox", { name: "Search projects" });
    expect(screen.getByText(/1 container ·/)).toBeTruthy();
    expect(screen.getByText(/0 containers ·/)).toBeTruthy();
    fireEvent.change(search, { target: { value: "  STORE  " } });
    expect(screen.getByRole("link", { name: /Storefront/ })).toBeTruthy();
    expect(screen.queryByRole("link", { name: /Documentation/ })).toBeNull();
    fireEvent.change(search, { target: { value: "missing" } });
    expect(screen.getByText("No projects match your search.")).toBeTruthy();
    fireEvent.change(search, { target: { value: "" } });
    expect(screen.getByRole("link", { name: /Documentation/ })).toBeTruthy();
    client.clear();
  });
});
