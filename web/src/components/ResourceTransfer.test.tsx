import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { UIProvider } from "../providers/UIProvider";
import { request } from "../services/api";
import { ResourceTransfer } from "./ResourceTransfer";

vi.mock("../layouts/DashboardLayout", () => ({
  useDashboard: () => ({
    workspaces: [
      { id: "from", name: "Source" },
      { id: "to", name: "Destination" },
    ],
  }),
}));
vi.mock("../providers/SessionProvider", () => ({
  useSession: () => ({ can: () => true }),
}));
vi.mock("../services/api", () => ({ request: vi.fn() }));

it("preselects linked resources and sends only the ones kept for copy", async () => {
  const showModal = HTMLDialogElement.prototype.showModal;
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
  try {
    vi.mocked(request)
      .mockResolvedValueOnce({
        plan_id: "plan-id",
        linked: [
          { kind: "services", id: "db", name: "Database", allowed: true },
          {
            kind: "volumes",
            id: "vol",
            name: "Database volume",
            allowed: true,
          },
        ],
      })
      .mockResolvedValueOnce({
        resources: [{ kind: "projects", id: "copy", name: "API" }],
        notes: [],
      });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <UIProvider>
            <ResourceTransfer
              kind="projects"
              id="p"
              name="API"
              workspaceId="from"
            />
          </UIProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Transfer" }));
    const database = await screen.findByRole("checkbox", {
      name: "Database Service",
    });
    const volume = screen.getByRole("checkbox", {
      name: "Database volume Volume",
    });
    expect(database).toBeChecked();
    expect(volume).toBeChecked();
    fireEvent.click(
      screen.getByRole("button", { name: "Destination workspace" }),
    );
    fireEvent.click(screen.getByText("Destination"));
    fireEvent.click(volume);
    fireEvent.click(screen.getByRole("button", { name: "Copy resources" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "transfers/copy",
        {
          resource: { kind: "projects", id: "p" },
          destination_workspace_id: "to",
          plan_id: "plan-id",
          include: [{ kind: "services", id: "db" }],
        },
        "POST",
      ),
    );
  } finally {
    HTMLDialogElement.prototype.showModal = showModal;
  }
});
