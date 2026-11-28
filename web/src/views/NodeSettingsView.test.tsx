import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LiveProvider } from "../providers/LiveProvider";
import { request } from "../services/api";
import type {
  DashboardConfig,
  DashboardSettings,
  NetworkInterface,
  RouterConfig,
  RouterSettings,
} from "../services/types";
import { NodeSettingsView } from "./NodeSettingsView";

const session = vi.hoisted(() => ({ user: { platform_admin: true } }));
vi.mock("../providers/SessionProvider", () => ({ useSession: () => session }));
vi.mock("../services/api", async (original) => ({
  ...(await original<typeof import("../services/api")>()),
  request: vi.fn(),
}));

let dashboardConfig: DashboardConfig;
let config: RouterConfig;
let interfaces: NetworkInterface[];
let error: Error | undefined;
let dashboardError: Error | undefined;

function renderView(
  live = false,
  regionName: "Router" | "Dashboard" = "Router",
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = <NodeSettingsView />;
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        {live ? <LiveProvider>{view}</LiveProvider> : view}
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return within(screen.getByRole("region", { name: regionName }));
}

beforeEach(() => {
  session.user.platform_admin = true;
  dashboardConfig = {
    dashboard_listen: [":8080"],
    config_path: "/etc/startship/config/startship.toml",
  };
  config = {
    enabled: true,
    http_listen: [":80"],
    https_listen: [":443"],
    config_path: "/etc/startship/config/startship.toml",
    overridden: [],
  };
  interfaces = [
    {
      name: "tailscale0",
      addresses: ["100.64.0.5"],
      up: true,
      loopback: false,
    },
    {
      name: "eth0",
      addresses: ["192.168.1.20"],
      up: true,
      loopback: false,
    },
    {
      name: "lo",
      addresses: ["127.0.0.1", "::1"],
      up: true,
      loopback: true,
    },
  ];
  error = undefined;
  dashboardError = undefined;
  vi.mocked(request).mockReset();
  vi.mocked(request).mockImplementation(async (path, body, method) => {
    if (path === "system/interfaces") {
      return { interfaces };
    }
    if (path === "system/dashboard") {
      if (method === "PUT") {
        if (dashboardError) throw dashboardError;
        dashboardConfig = {
          ...dashboardConfig,
          ...(body as DashboardSettings),
        };
      }
      return dashboardConfig;
    }
    if (path === "system/router") {
      if (method === "PUT") {
        if (error) throw error;
        config = { ...config, ...(body as RouterSettings) };
      }
      return config;
    }
    return path === "system/version"
      ? { version: "v1", commit: "abc" }
      : { current: "v1", development: true };
  });
});
afterEach(() => vi.unstubAllGlobals());

describe("node dashboard listen settings", () => {
  it("switches between a detected interface, any interface, and custom addresses", async () => {
    const panel = renderView(false, "Dashboard");
    const user = userEvent.setup();
    const trigger = await panel.findByRole("button", {
      name: "Dashboard listen addresses interface",
    });
    expect(trigger).toHaveTextContent("Any interface");
    const save = panel.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();

    await user.click(trigger);
    await user.click(
      panel.getByRole("option", { name: /tailscale0.*100\.64\.0\.5/ }),
    );
    expect(trigger).toHaveTextContent("tailscale0");
    expect(
      panel.getByText(/Saving closes the previous dashboard listener/),
    ).toBeInTheDocument();
    await user.click(save);
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "system/dashboard",
        { dashboard_listen: ["tailscale0:8080"] },
        "PUT",
      ),
    );

    const port = panel.getByRole("textbox", {
      name: "Dashboard listen addresses port",
    });
    await user.clear(port);
    await user.type(port, "9090");
    await user.click(trigger);
    await user.click(panel.getByRole("option", { name: /Any interface/ }));
    await user.click(save);
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "system/dashboard",
        { dashboard_listen: [":9090"] },
        "PUT",
      ),
    );

    await user.click(trigger);
    await user.click(panel.getByRole("option", { name: /Custom addresses/ }));
    const customInput = panel.getByRole("textbox", {
      name: "Dashboard listen addresses",
    });
    await user.clear(customInput);
    await user.type(customInput, "tailscale0:8080, 127.0.0.1:8080");
    await user.click(save);
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "system/dashboard",
        { dashboard_listen: ["tailscale0:8080", "127.0.0.1:8080"] },
        "PUT",
      ),
    );
  });

  it("allows platform admins to edit dashboard interface and port", async () => {
    const panel = renderView(false, "Dashboard");
    const trigger = await panel.findByRole("button", {
      name: "Dashboard listen addresses interface",
    });
    expect(trigger).toBeEnabled();
    const port = panel.getByRole("textbox", {
      name: "Dashboard listen addresses port",
    });
    expect(port).toBeEnabled();
    expect(panel.queryByText(/Set at startup by/)).not.toBeInTheDocument();
  });

  it("renders dashboard settings read-only for non-admins", async () => {
    session.user.platform_admin = false;
    dashboardConfig = {
      ...dashboardConfig,
      dashboard_listen: ["tailscale0:8080"],
    };
    const panel = renderView(false, "Dashboard");
    const trigger = await panel.findByRole("button", {
      name: "Dashboard listen addresses interface",
    });
    expect(trigger).toBeDisabled();
    expect(trigger).toHaveTextContent("tailscale0");
    expect(
      panel.getByRole("textbox", { name: "Dashboard listen addresses port" }),
    ).toHaveAttribute("readonly");
    expect(
      panel.queryByRole("button", { name: "Save" }),
    ).not.toBeInTheDocument();
  });
});

describe("node router settings", () => {
  it("selects an interface, saves custom listens, and disables and re-enables the router", async () => {
    const panel = renderView();
    const user = userEvent.setup();
    const httpTrigger = await panel.findByRole("button", {
      name: "HTTP listen addresses interface",
    });
    expect(panel.getByText(config.config_path)).toBeInTheDocument();
    const save = panel.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();

    await user.click(httpTrigger);
    const httpMenu = panel.getByRole("listbox", {
      name: "HTTP listen addresses interface",
    });
    await user.click(
      within(httpMenu).getByRole("option", { name: /Custom addresses/ }),
    );
    const http = panel.getByRole("textbox", {
      name: "HTTP listen addresses",
    });
    await user.clear(http);
    await user.type(http, "127.0.0.1:8081, eth0:8082");
    await user.click(save);
    const listeners = {
      http_listen: ["127.0.0.1:8081", "eth0:8082"],
      https_listen: [":443"],
    };
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "system/router",
        { enabled: true, ...listeners },
        "PUT",
      ),
    );
    const toggle = panel.getByRole("switch");
    await user.click(toggle);
    expect(
      panel.getByText(/dashboard stays available on its own port/),
    ).toBeInTheDocument();
    expect(panel.getByText("Enabled")).toBeInTheDocument();
    await user.click(save);
    expect(await panel.findByText("Disabled")).toBeInTheDocument();
    expect(request).toHaveBeenCalledWith(
      "system/router",
      { enabled: false, ...listeners },
      "PUT",
    );
    await user.click(toggle);
    await user.click(save);
    expect(await panel.findByText("Enabled")).toBeInTheDocument();
  });

  it("renders empty lists and disabled state read-only for non-admins", async () => {
    session.user.platform_admin = false;
    config = { ...config, enabled: false, http_listen: [], https_listen: [] };
    const panel = renderView();
    expect(await panel.findByText("Disabled")).toBeInTheDocument();
    for (const input of panel.getAllByRole("textbox")) {
      expect(input).toHaveAttribute("readonly");
      expect(input).toHaveValue("");
    }
    expect(panel.queryByRole("switch")).not.toBeInTheDocument();
    expect(
      panel.queryByRole("button", { name: "Save" }),
    ).not.toBeInTheDocument();
  });

  it.each([
    "proxy: cannot listen on :8888 (address already in use)",
    "router port 8888 collides with the dashboard",
  ])(
    "keeps listeners editable when the router toggle is locked and displays the API error: %s",
    async (message) => {
      config.overridden = ["router_disabled"];
      error = new Error(message);
      const panel = renderView();
      const toggle = await panel.findByRole("switch");
      expect(toggle).toBeDisabled();
      expect(toggle).toHaveAccessibleDescription(
        /STARTSHIP_ROUTER_DISABLED.*--router-disabled/,
      );
      const httpsPort = panel.getByRole("textbox", {
        name: "HTTPS listen addresses port",
      });
      expect(httpsPort).toBeEnabled();
      const user = userEvent.setup();
      const httpPort = panel.getByRole("textbox", {
        name: "HTTP listen addresses port",
      });
      await user.clear(httpPort);
      await user.type(httpPort, "8888");
      await user.click(panel.getByRole("button", { name: "Save" }));
      expect(await panel.findByRole("alert")).toHaveTextContent(error.message);
      expect(httpPort).toHaveValue("8888");
    },
  );

  it("uses live snapshots rather than refetching and keeps an in-progress edit", async () => {
    let socket: Socket;
    class Socket {
      static OPEN = 1;
      readyState = 1;
      send = vi.fn();
      close = vi.fn();
      onmessage?: (event: { data: string }) => void;
      constructor() {
        socket = this;
      }
      receive(message: object) {
        this.onmessage?.({ data: JSON.stringify(message) });
      }
    }
    vi.stubGlobal("WebSocket", Socket);
    const panel = renderView(true);
    const httpPort = await panel.findByRole("textbox", {
      name: "HTTP listen addresses port",
    });
    const user = userEvent.setup();
    await user.clear(httpPort);
    await user.type(httpPort, "8081");
    act(() => socket.receive({ type: "ready" }));
    const subscriptions = socket!.send.mock.calls.map(([msg]) =>
      JSON.parse(msg as string),
    );
    const routerSub = subscriptions.find((s) => s.topic === "system_router");
    const dashboardSub = subscriptions.find(
      (s) => s.topic === "system_dashboard",
    );
    expect(routerSub).toMatchObject({
      type: "subscribe",
      topic: "system_router",
    });
    expect(dashboardSub).toMatchObject({
      type: "subscribe",
      topic: "system_dashboard",
    });
    act(() =>
      socket.receive({
        type: "snapshot",
        id: routerSub.id,
        topic: "system_router",
        data: { ...config, enabled: false, https_listen: [":8443"] },
      }),
    );
    expect(await panel.findByText("Disabled")).toBeInTheDocument();
    expect(httpPort).toHaveValue("8081");
    expect(
      panel.getByRole("textbox", { name: "HTTPS listen addresses port" }),
    ).toHaveValue("8443");
    expect(
      vi
        .mocked(request)
        .mock.calls.filter(([path]) => path === "system/router"),
    ).toHaveLength(1);
  });
});
