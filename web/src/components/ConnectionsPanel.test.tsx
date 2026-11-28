import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { request } from "../services/api";
import type { Connection } from "../services/types";
import { ConnectionsPanel, ConnectModal } from "./ConnectionsPanel";

vi.mock("../services/api", () => ({ request: vi.fn(), remove: vi.fn() }));
vi.mock("../providers/LiveProvider", () => ({
  useLiveSubscription: () => undefined,
}));
vi.mock("../providers/UIProvider", () => ({
  useUI: () => ({ run: vi.fn(), notify: vi.fn(), dialog: vi.fn() }),
}));

const link: Connection = {
  id: "c1",
  workspace_id: "w",
  from: { kind: "project", id: "p" },
  to: { kind: "service", id: "s" },
  alias: "db",
  env: { DB_HOST: "db.local" },
  created: "",
  created_by: "",
};
const data: Record<string, unknown> = {
  "projects?workspace_id=w": [
    { id: "p", name: "Storefront", workspace_id: "w" },
  ],
  "workspaces/w/apps": {
    apps: [{ id: "a", name: "Media Box", recipe: { name: "Jellyfin" } }],
  },
  "workspaces/w/services": {
    services: [
      {
        id: "s",
        name: "primary",
        engine: "postgresql",
        version: "17",
        config: {},
      },
    ],
  },
  "workspaces/w/connections": { connections: [link] },
  catalog: [],
};
function wrap(ui: React.ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
});
beforeEach(() => {
  vi.mocked(request).mockReset();
  vi.mocked(request).mockImplementation(async (path: string) => {
    if (path in data) return data[path];
    return {};
  });
});

describe("connections", () => {
  it("lists outgoing and incoming connections for an endpoint", async () => {
    wrap(
      <ConnectionsPanel
        workspaceId="w"
        endpoint={{ kind: "project", id: "p" }}
        canManage
      />,
    );
    expect(await screen.findByText("primary")).toBeInTheDocument();
    expect(screen.getAllByText("db.local").length).toBeGreaterThan(0);
    expect(
      screen.getByRole("button", { name: "Disconnect db" }),
    ).toBeInTheDocument();
  });
  it("shows isolation when nothing is connected", async () => {
    wrap(
      <ConnectionsPanel
        workspaceId="w"
        endpoint={{ kind: "app", id: "a" }}
        canManage={false}
      />,
    );
    expect(await screen.findByText(/Isolated\./)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New connection" })).toBeNull();
  });
  it("connects an app to a project with a derived alias", async () => {
    const user = userEvent.setup(),
      saved = vi.fn();
    wrap(
      <ConnectModal
        workspaceId="w"
        endpoint={{ kind: "app", id: "a" }}
        connections={[link]}
        onClose={() => {}}
        onSaved={saved}
      />,
    );
    await user.click(await screen.findByRole("button", { name: /Storefront/ }));
    expect(screen.getByLabelText("Hostname alias")).toHaveValue("storefront");
    expect(screen.getAllByText("STOREFRONT_HOST").length).toBeGreaterThan(0);
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("workspaces/w/connections", {
        from: { kind: "app", id: "a" },
        to: { kind: "project", id: "p" },
        alias: "storefront",
      }),
    );
    expect(saved).toHaveBeenCalled();
  });
  it("disables pairs that already exist and flags env prefix clashes", async () => {
    const user = userEvent.setup();
    wrap(
      <ConnectModal
        workspaceId="w"
        endpoint={{ kind: "project", id: "p" }}
        connections={[link]}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    );
    expect(
      await screen.findByRole("button", { name: /primary/ }),
    ).toBeDisabled();
    await user.click(screen.getByRole("button", { name: /Media Box/ }));
    const alias = screen.getByLabelText("Hostname alias");
    await user.clear(alias);
    await user.type(alias, "db");
    expect(screen.getByText(/already in use/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();
  });
  it("defaults services to incoming connections", async () => {
    const user = userEvent.setup();
    wrap(
      <ConnectModal
        workspaceId="w"
        endpoint={{ kind: "service", id: "s" }}
        connections={[]}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    );
    await user.click(await screen.findByRole("button", { name: /Media Box/ }));
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("workspaces/w/connections", {
        from: { kind: "app", id: "a" },
        to: { kind: "service", id: "s" },
        alias: "primary",
      }),
    );
  });
});
