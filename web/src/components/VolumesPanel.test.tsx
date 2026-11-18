import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { beforeAll, beforeEach, describe, it, expect, vi } from "vitest";
import { request } from "../services/api";
import { VolumesPanel } from "./VolumesPanel";
import type { Project, Volume } from "../services/types";
vi.mock("../services/api", () => ({ request: vi.fn(), remove: vi.fn() }));
vi.mock("../layouts/DashboardLayout", () => ({
  useDashboard: () => ({ workspace: { id: "w", name: "Workspace" } }),
}));
vi.mock("../components/ProjectHeading", () => ({
  ProjectHeading: ({
    title,
    actions,
  }: {
    title: string;
    actions: React.ReactNode;
  }) => (
    <header>
      <h1>{title}</h1>
      {actions}
    </header>
  ),
}));
const state = vi.hoisted(() => ({ allowed: true }));
vi.mock("../providers/SessionProvider", () => ({
  useSession: () => ({ can: () => state.allowed }),
}));
vi.mock("../providers/UIProvider", () => ({
  useUI: () => ({ notify: vi.fn(), run: vi.fn() }),
}));
const project: Project = { id: "p", name: "App", workspace_id: "w" };
let rows: Volume[] = [];
beforeAll(() => {
  Object.defineProperty(HTMLDialogElement.prototype, "showModal", {
    configurable: true,
    value: function (this: HTMLDialogElement) {
      this.setAttribute("open", "");
    },
  });
  for (const method of ["showPopover", "hidePopover"])
    Object.defineProperty(HTMLElement.prototype, method, {
      configurable: true,
      value: vi.fn(),
    });
});
beforeEach(() => {
  state.allowed = true;
  rows = [];
  vi.mocked(request).mockReset();
  vi.mocked(request).mockImplementation(async (path, body) => {
    if (body !== undefined) return {};
    if (path === "workspaces/w/volumes") return { volumes: rows };
    if (path === "workspaces/w/services")
      return { services: [{ id: "s", name: "db", engine: "postgresql" }] };
    if (path === "catalog")
      return [{ id: "postgresql", data_path: "/var/lib/postgresql/data" }];
    return [];
  });
});
function view(p?: Project) {
  return render(
    <MemoryRouter>
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <VolumesPanel workspaceId="w" project={p} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}
describe("volume management", () => {
  it("creates a workspace volume and renders no native selects", async () => {
    const user = userEvent.setup(),
      { container } = view();
    await screen.findByText("Storage that stays");
    await user.click(
      screen.getAllByRole("button", { name: "Create volume" })[0],
    );
    await user.type(screen.getByLabelText("Volume name"), "uploads");
    await user.click(
      screen.getAllByRole("button", { name: "Create volume" }).at(-1)!,
    );
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("workspaces/w/volumes", {
        name: "uploads",
      }),
    );
    expect(container.querySelector("select")).toBeNull();
  });
  it("shows service mount paths and disables deletion while in use", async () => {
    rows = [
      {
        id: "v",
        name: "db-data",
        service_id: "s",
        created: "2026-01-01",
        size_bytes: 1024,
        size_updated: "2026-01-01",
      },
    ] as Volume[];
    view();
    expect(
      await screen.findByText("/var/lib/postgresql/data"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Delete db-data" }),
    ).toBeDisabled();
    expect(screen.getByText("1.0 KB")).toBeInTheDocument();
  });
  it("attaches an unused workspace volume to a project", async () => {
    rows = [
      {
        id: "v",
        name: "backups",
        size_bytes: 0,
        created: "2026-01-01",
        project_id: "",
        service_id: "",
      },
    ] as Volume[];
    const user = userEvent.setup(),
      { container } = view(project);
    await screen.findByText("Give your files a permanent home");
    await user.click(screen.getAllByRole("button", { name: "Add volume" })[0]);
    await user.click(screen.getByRole("button", { name: "Attach existing" }));
    await user.click(
      screen.getByRole("option", { name: /backups/, hidden: true }),
    );
    await user.click(screen.getByRole("button", { name: "Attach volume" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "volumes/v",
        { project_id: "p", mount_path: "/data" },
        "PATCH",
      ),
    );
    expect(await screen.findByText("Redeploy to apply")).toBeInTheDocument();
    expect(container.querySelector("select")).toBeNull();
  });
  it("shows query errors", async () => {
    vi.mocked(request).mockRejectedValue(new Error("Storage unavailable"));
    view();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Storage unavailable",
    );
  });
  it("hides mutations without resource permissions", async () => {
    state.allowed = false;
    view();
    await screen.findByText("Storage that stays");
    expect(screen.queryByRole("button", { name: "Create volume" })).toBeNull();
  });
});
