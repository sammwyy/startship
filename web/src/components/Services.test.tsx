import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { request } from "../services/api";
import type { ServiceEngine } from "../services/types";
import { NewServiceModal } from "./NewServiceModal";
import { EnvPreview } from "./ServiceUI";
import { EngineIcon } from "./EngineIcon";
import { generateServiceSecret } from "./ServiceConfig";
vi.mock("../services/api", () => ({ request: vi.fn() }));
it("generates secrets accepted by engines requiring mixed password classes", () => {
  expect(generateServiceSecret()).toMatch(/^Ss1![a-f0-9]{48}$/);
});
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
beforeAll(() => {
  Object.defineProperty(HTMLDialogElement.prototype, "showModal", {
    configurable: true,
    value: function (this: HTMLDialogElement) {
      this.setAttribute("open", "");
    },
  });
  for (const method of ["showPopover", "hidePopover", "scrollIntoView"])
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
  it("filters the modal marketplace by category", async () => {
    vi.mocked(request).mockImplementation(async (path) =>
      path === "catalog/status"
        ? { source: "bundled" }
        : [
            engine,
            {
              ...engine,
              id: "redis",
              name: "Redis",
              category: "cache",
              description: "In-memory data store.",
            },
            {
              ...engine,
              id: "prometheus",
              name: "Prometheus",
              category: "observability",
              description: "Time-series monitoring.",
            },
          ],
    );
    catalog();
    expect(
      screen.getByRole("dialog", { name: "Service marketplace" }),
    ).toBeInTheDocument();
    await userEvent.click(
      await screen.findByRole("button", { name: "Caches" }),
    );
    expect(screen.getByRole("button", { name: /Redis/ })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /PostgreSQL/ }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Observability" }),
    );
    expect(
      screen.getByRole("button", { name: /Prometheus/ }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Redis/ })).toBeNull();
  });
  it("uses a compact browse header and keeps the configuration header", async () => {
    vi.mocked(request).mockImplementation(async (path) =>
      path === "catalog/status" ? { source: "bundled" } : [engine],
    );
    catalog();
    const dialog = screen.getByRole("dialog", { name: "Service marketplace" });
    const browse = within(dialog);
    expect(dialog).toHaveClass("service-marketplace");
    expect(
      browse.getByRole("button", { name: "Close dialog" }),
    ).toBeInTheDocument();
    expect(browse.queryByText("ADD SERVICE")).not.toBeInTheDocument();
    expect(
      browse.queryByRole("button", { name: "Cancel" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      await browse.findByRole("button", { name: /PostgreSQL/ }),
    );
    const config = screen.getByRole("dialog", { name: "Configure PostgreSQL" });
    expect(config).not.toHaveClass("service-marketplace");
    expect(
      within(config).getByText(/we’ll handle the container/i),
    ).toBeInTheDocument();
  });
  it("refreshes the catalog and shows the cached source and timestamp", async () => {
    const updated = "2026-01-01T12:00:00Z";
    vi.mocked(request).mockImplementation(async (path) => {
      if (path === "catalog/status") return { source: "bundled" };
      if (path === "catalog/refresh?workspace=w")
        return {
          entries: [{ ...engine, name: "Remote PostgreSQL" }],
          metadata: { source: "remote", updated_at: updated },
        };
      return [engine];
    });
    catalog();
    expect(await screen.findByText("Bundled catalog")).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Refresh catalog" }),
    );
    expect(await screen.findByText(/Remote cache/)).toBeInTheDocument();
    expect(
      screen.getByText(/Remote cache/).querySelector("time"),
    ).toHaveAttribute("datetime", updated);
    expect(
      screen.getByRole("button", { name: /Remote PostgreSQL/ }),
    ).toBeInTheDocument();
    expect(request).toHaveBeenCalledWith(
      "catalog/refresh?workspace=w",
      undefined,
      "POST",
    );
  });
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
    vi.mocked(request).mockImplementation(async (path) => {
      if (path === "catalog/status") return { source: "bundled" };
      if (path === "catalog") return [engine];
      throw new Error("Port already reserved");
    });
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
describe("connect picker", () => {
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
