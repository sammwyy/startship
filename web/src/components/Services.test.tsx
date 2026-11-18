import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { request } from "../services/api";
import type { Service, ServiceEngine, Project } from "../services/types";
import { NewServiceModal } from "./NewServiceModal";
import { ConnectServiceModal } from "./ConnectServiceModal";
import { EnvPreview } from "./ServiceUI";
import { EngineIcon } from "./EngineIcon";
vi.mock("../services/api", () => ({ request: vi.fn() }));
vi.mock("../providers/UIProvider", () => ({
  useUI: () => ({ run: vi.fn(), notify: vi.fn() }),
}));
const engine: ServiceEngine = {
  id: "postgresql",
  icon: "postgresql",
  name: "PostgreSQL",
  category: "database",
  description: "PostgreSQL database service.",
  image: "postgres",
  versions: ["17", "16"],
  port: 5432,
  data_path: "/var/lib/postgresql/data",
  fields: [
    {
      key: "password",
      label: "Password",
      type: "password",
      default: "",
      generate: true,
      required: true,
      help: "",
    },
  ],
};
const projectA: Project = { id: "a", name: "Storefront", workspace_id: "w" },
  projectB: Project = { id: "b", name: "Workers", workspace_id: "w" };
const service: Service = {
  id: "s",
  workspace_id: "w",
  name: "primary",
  engine: "postgresql",
  version: "17",
  config: { user: "app", password: "private", database: "app" },
  external_port: 0,
  volume_id: "v",
  attachments: [{ project_id: "a", alias: "db" }],
  status: "running",
  last_error: "",
  container: "",
  created: "",
  updated: "",
  created_by: "",
};
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
  vi.mocked(request).mockReset();
});
function catalog() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <NewServiceModal
        workspaceId="w"
        onClose={vi.fn()}
        onSaved={vi.fn().mockResolvedValue(undefined)}
      />
    </QueryClientProvider>,
  );
}
describe("service catalog", () => {
  it("uses readable dark brand marks and a memcached monogram", () => {
    const { container } = render(
      <>
        <EngineIcon engine="kafka" />
        <EngineIcon engine="memcached" />
      </>,
    );
    expect(
      container.querySelector(".engine-icon-neutral svg"),
    ).toBeInTheDocument();
    expect(screen.getByText("mc")).toBeInTheDocument();
  });
  it("searches, configures and deploys without native selects", async () => {
    vi.mocked(request).mockResolvedValue([engine]);
    const user = userEvent.setup(),
      { container } = catalog();
    await user.type(
      screen.getByRole("textbox", { name: "Search service catalog" }),
      "postgres",
    );
    await user.click(await screen.findByRole("button", { name: /PostgreSQL/ }));
    expect(screen.getByLabelText("Service name")).toHaveValue("postgresql");
    expect(screen.getByRole("button", { name: "Version" })).toHaveTextContent(
      "17",
    );
    expect(screen.getByLabelText("Password")).toHaveAttribute(
      "type",
      "password",
    );
    await user.click(screen.getByRole("button", { name: "Show Password" }));
    expect(screen.getByLabelText("Password")).toHaveAttribute("type", "text");
    expect(screen.getByText("Persistent by default")).toBeInTheDocument();
    await user.clear(screen.getByLabelText("Service name"));
    await user.type(screen.getByLabelText("Service name"), "bad name");
    expect(
      screen.getByRole("button", { name: "Deploy service" }),
    ).toBeDisabled();
    await user.clear(screen.getByLabelText("Service name"));
    await user.type(screen.getByLabelText("Service name"), "primary-db");
    await user.click(screen.getByRole("switch"));
    await user.type(screen.getByLabelText("External port"), "15432");
    expect(container.querySelector("select")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Deploy service" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "workspaces/w/services",
        expect.objectContaining({
          engine: "postgresql",
          name: "primary-db",
          version: "17",
          external_port: 15432,
          config: { password: expect.any(String) },
        }),
      ),
    );
  });
  it("returns to catalog and shows no persistence for memcached", async () => {
    vi.mocked(request).mockResolvedValue([
      {
        ...engine,
        id: "memcached",
        name: "Memcached",
        category: "cache",
        fields: null,
        data_path: "",
        versions: ["1.6"],
      },
    ]);
    const user = userEvent.setup();
    catalog();
    await user.click(await screen.findByRole("button", { name: /Memcached/ }));
    expect(screen.getByText("No persistence")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Back to catalog" }));
    expect(
      screen.getByRole("textbox", { name: "Search service catalog" }),
    ).toBeInTheDocument();
  });
  it("handles an empty search", async () => {
    vi.mocked(request).mockResolvedValue([engine]);
    const user = userEvent.setup();
    catalog();
    await screen.findByRole("button", { name: /PostgreSQL/ });
    await user.type(
      screen.getByRole("textbox", { name: "Search service catalog" }),
      "missing",
    );
    expect(screen.getByText(/No services match/)).toBeInTheDocument();
  });
  it("shows catalog failures", async () => {
    vi.mocked(request).mockRejectedValue(new Error("Catalog unavailable"));
    catalog();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Catalog unavailable",
    );
  });
  it("keeps configuration after a deployment error", async () => {
    vi.mocked(request)
      .mockResolvedValueOnce([engine])
      .mockRejectedValueOnce(new Error("Port already reserved"));
    const user = userEvent.setup();
    catalog();
    await user.click(await screen.findByRole("button", { name: /PostgreSQL/ }));
    await user.click(screen.getByRole("button", { name: "Deploy service" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Port already reserved",
    );
    expect(screen.getByLabelText("Service name")).toHaveValue("postgresql");
  });
});
function connect(services: Service[] = [service]) {
  return render(
    <MemoryRouter>
      <ConnectServiceModal
        workspaceId="w"
        services={services}
        engines={[engine]}
        projects={[projectA, projectB]}
        project={projectB}
        onClose={vi.fn()}
        onSaved={vi.fn().mockResolvedValue(undefined)}
      />
    </MemoryRouter>,
  );
}
describe("connect picker", () => {
  it("shows attachments before selection and previews the exact alias host", async () => {
    vi.mocked(request).mockResolvedValue({});
    const user = userEvent.setup(),
      { container } = connect();
    expect(screen.getByText("Attached to: Storefront")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /primary/ }));
    expect(screen.getByLabelText("Connection alias")).toHaveValue("primary");
    await user.clear(screen.getByLabelText("Connection alias"));
    await user.type(screen.getByLabelText("Connection alias"), "app_db");
    expect(container.querySelector(".env-line")).toHaveTextContent(
      "app_db.local",
    );
    expect(
      Array.from(
        container.querySelectorAll(".env-line > span:first-child"),
        (node) => node.textContent,
      ),
    ).toEqual([
      "APP_DB_HOST",
      "APP_DB_PORT",
      "APP_DB_USER",
      "APP_DB_PASSWORD",
      "APP_DB_DATABASE",
      "APP_DB_URL",
    ]);
    await user.click(screen.getByRole("button", { name: "Show secrets" }));
    expect(container.querySelector(".env-line:last-child")).toHaveTextContent(
      "postgres://app:private@app_db.local:5432/app",
    );
    expect(container.querySelector("select")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Connect service" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("services/s/attachments", {
        project_id: "b",
        alias: "app_db",
      }),
    );
  });
  it("handles an empty list with a deploy link", () => {
    connect([]);
    expect(screen.getByText(/No workspace services/)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Deploy a new service" }),
    ).toHaveAttribute("href", "/workspace/w/services");
    expect(
      screen.getByRole("button", { name: "Connect service" }),
    ).toBeDisabled();
  });
  it("keeps already-connected services visible and disabled", async () => {
    const user = userEvent.setup();
    connect([
      service,
      {
        ...service,
        id: "taken",
        name: "cache",
        attachments: [{ project_id: "b", alias: "cache" }],
      },
    ]);
    const attached = screen.getByRole("button", {
      name: /cache.*Connected to this project/,
    });
    expect(attached).toBeDisabled();
    expect(attached).toHaveTextContent("Attached to: Workers");
    expect(screen.getByLabelText("Connection alias")).toBeDisabled();
    expect(
      screen.getByText(
        "Choose a service to preview its connection environment.",
      ),
    ).toBeInTheDocument();
    await user.click(attached);
    expect(
      screen.getByRole("button", { name: "Connect service" }),
    ).toBeDisabled();
    await user.click(screen.getByRole("button", { name: /primary/ }));
    expect(screen.getByLabelText("Connection alias")).toHaveValue("primary");
    expect(
      screen.getByRole("button", { name: "Connect service" }),
    ).toBeEnabled();
  });
  it("shows attach errors without closing", async () => {
    vi.mocked(request).mockRejectedValue(new Error("Alias already used"));
    const user = userEvent.setup();
    connect();
    await user.click(screen.getByRole("button", { name: /primary/ }));
    await user.click(screen.getByRole("button", { name: "Connect service" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Alias already used",
    );
  });
  it("masks secrets until revealed", async () => {
    const user = userEvent.setup();
    render(
      <EnvPreview
        env={{
          DB_HOST: "db.local",
          DB_PASSWORD: "private",
          DB_URL: "postgres://u:private@db.local:5432/db",
        }}
        canReveal
      />,
    );
    expect(screen.queryByText("private")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Show secrets" }));
    expect(screen.getByText("private")).toBeInTheDocument();
  });
});
