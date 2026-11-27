import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { UIProvider } from "../../providers/UIProvider";
import { request } from "../../services/api";
import type { RecipeApps } from "../../services/recipeApps";
import { RecipeAppsView } from "./RecipeAppsView";

vi.mock("../../layouts/DashboardLayout", () => ({
  useDashboard: () => ({ workspace: { id: "w" } }),
}));
vi.mock("../../providers/LiveProvider", () => ({
  useLiveSubscription: () => undefined,
}));
vi.mock("../../services/api", () => ({ request: vi.fn(), remove: vi.fn() }));

it.each([
  { status: "exited", running: false, badge: "exited" },
  { status: "running", running: true, badge: "unhealthy" },
])(
  "shows $badge for a $status container with unhealthy health",
  async ({ status, running, badge }) => {
    vi.mocked(request).mockResolvedValue({
      can_manage: false,
      apps: [
        {
          id: "app",
          name: "Status page",
          recipe: {
            id: "status-page",
            name: "Status Page",
            description: "",
            icon: "box",
            version: "1",
            config_schema: { properties: {} },
            containers: [{ id: "web", image: "busybox" }],
            volumes: null,
            http: null,
            ports: null,
            files: null,
          },
          status: running ? "degraded" : "stopped",
          running,
          config: null,
          ports: {},
          domains: [],
          containers: { web: { status, health: "unhealthy" } },
        },
      ],
    } satisfies RecipeApps);

    const client = new QueryClient();
    render(
      <QueryClientProvider client={client}>
        <UIProvider>
          <MemoryRouter initialEntries={["/workspace/w/apps/app"]}>
            <Routes>
              <Route
                path="/workspace/:workspaceId/apps/:appId"
                element={<RecipeAppsView />}
              />
            </Routes>
          </MemoryRouter>
        </UIProvider>
      </QueryClientProvider>,
    );

    expect(await screen.findByText(badge)).toBeInTheDocument();
  },
);
