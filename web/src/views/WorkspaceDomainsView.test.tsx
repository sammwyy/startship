import { Suspense } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UIProvider } from "../providers/UIProvider";
import { request } from "../services/api";
import { WorkspaceDomainsView } from "./WorkspaceDomainsView";

vi.mock("../services/api", () => ({ request: vi.fn(), remove: vi.fn() }));
let canManage = true;
beforeEach(() => {
  vi.clearAllMocks();
  canManage = true;
  vi.mocked(request).mockImplementation(async () => ({
    can_manage: canManage,
    domains: [{ id: "d1", name: "example.com", subdomains: true, routes: 2 }],
  }));
});
function renderView() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  render(
    <QueryClientProvider client={client}>
      <UIProvider>
        <MemoryRouter initialEntries={["/workspace/team/domains"]}>
          <Suspense fallback="Loading…">
            <Routes>
              <Route
                path="/workspace/:workspaceId/domains"
                element={<WorkspaceDomainsView />}
              />
            </Routes>
          </Suspense>
        </MemoryRouter>
      </UIProvider>
    </QueryClientProvider>,
  );
}
describe("workspace domains", () => {
  it("lists and registers allowed domains in the current workspace, separate from settings", async () => {
    renderView();
    await screen.findByRole("heading", { name: "Domains" });
    expect(request).toHaveBeenCalledWith("workspaces/team/domains");
    expect(
      screen.getByText("example.com", { selector: ".domain-name" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Workspace name")).not.toBeInTheDocument();
    await userEvent.type(
      screen.getByRole("textbox", { name: "Domain" }),
      "new.example.com",
    );
    await userEvent.click(screen.getByRole("button", { name: "Add domain" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("workspaces/team/domains", {
        name: "new.example.com",
        subdomains: false,
      }),
    );
  });
  it("keeps domains visible without exposing management controls to read-only members", async () => {
    canManage = false;
    renderView();
    await screen.findByRole("heading", { name: "Domains" });
    expect(
      screen.getByText("example.com", { selector: ".domain-name" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Add domain" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Remove" }),
    ).not.toBeInTheDocument();
  });
});
