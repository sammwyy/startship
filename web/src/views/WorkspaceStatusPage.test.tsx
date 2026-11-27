import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { useLiveFeed } from "../providers/LiveProvider";
import { UIProvider } from "../providers/UIProvider";
import { request } from "../services/api";
import { StatusPageForm, WorkspaceSettingsView } from "./WorkspaceSettingsView";

vi.mock("../services/api", () => ({ request: vi.fn().mockResolvedValue({}) }));
vi.mock("../layouts/DashboardLayout", () => ({
  useDashboard: () => ({ workspace: { id: "ws", name: "Team" } }),
}));
vi.mock("../providers/SessionProvider", () => ({
  useSession: () => ({ can: () => false }),
}));
vi.mock("../providers/LiveProvider", () => ({
  useLiveFeed: vi.fn(),
  useLiveSubscription: vi.fn(),
}));
afterEach(cleanup);

it("publishes only the projects picked in the workspace form", async () => {
  const user = userEvent.setup();
  render(
    <StatusPageForm
      id="ws"
      settings={{ enabled: false, slug: "", title: "", projects: [] }}
      projects={[
        { id: "api", name: "API", workspace_id: "ws", uptime_monitoring: true },
        { id: "private", name: "Private", workspace_id: "ws" },
      ]}
      onSaved={() => {}}
    />,
  );
  await user.click(
    screen.getByRole("checkbox", { name: "Publish status page" }),
  );
  await user.type(screen.getByLabelText("Page title"), "Product status");
  await user.type(screen.getByLabelText("Address slug"), "product-status");
  await user.click(screen.getByRole("checkbox", { name: /API/ }));
  await user.click(screen.getByRole("button", { name: "Save status page" }));
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith(
      "workspaces/ws/status-page",
      {
        enabled: true,
        slug: "product-status",
        title: "Product status",
        projects: ["api"],
      },
      "PATCH",
    ),
  );
});

it("hides notifications and never subscribes for a workspace member without manage permission", () => {
  const client = new QueryClient();
  render(
    <QueryClientProvider client={client}>
      <UIProvider>
        <MemoryRouter initialEntries={["/workspace/ws/settings"]}>
          <Routes>
            <Route
              path="/workspace/:workspaceId/settings"
              element={<WorkspaceSettingsView />}
            />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </QueryClientProvider>,
  );
  expect(
    screen.getByRole("heading", { name: "Workspace settings" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "Notifications" }),
  ).not.toBeInTheDocument();
  expect(useLiveFeed).not.toHaveBeenCalled();
  client.clear();
});
