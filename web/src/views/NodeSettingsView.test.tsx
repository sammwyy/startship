import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LiveProvider } from "../providers/LiveProvider";
import { request } from "../services/api";
import type { RouterConfig, RouterSettings } from "../services/types";
import { NodeSettingsView } from "./NodeSettingsView";

const session = vi.hoisted(() => ({ user: { platform_admin: true } }));
vi.mock("../providers/SessionProvider", () => ({ useSession: () => session }));
vi.mock("../services/api", async (original) => ({
  ...(await original<typeof import("../services/api")>()),
  request: vi.fn(),
}));

let config: RouterConfig;
let error: Error | undefined;

function renderView(live = false) {
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
  return within(screen.getByRole("region", { name: "Router" }));
}

beforeEach(() => {
  session.user.platform_admin = true;
  config = {
    enabled: true,
    http_listen: [":80"],
    https_listen: [":443"],
    config_path: "/etc/startship/config/startship.toml",
    overridden: [],
  };
  error = undefined;
  vi.mocked(request).mockReset();
  vi.mocked(request).mockImplementation(async (path, body, method) => {
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

describe("node router settings", () => {
  it("saves comma-separated listens and disables and re-enables the router", async () => {
    const panel = renderView();
    const user = userEvent.setup();
    const http = await panel.findByRole("textbox", {
      name: "HTTP listen addresses",
    });
    expect(panel.getByText(config.config_path)).toBeInTheDocument();
    const save = panel.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();
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
    "locks overridden fields and displays the API error: %s",
    async (message) => {
      config.overridden = ["router_disabled", "https_listen"];
      error = new Error(message);
      const panel = renderView();
      const toggle = await panel.findByRole("switch");
      expect(toggle).toBeDisabled();
      expect(toggle).toHaveAccessibleDescription(
        /STARTSHIP_ROUTER_DISABLED.*--router-disabled/,
      );
      const https = panel.getByRole("textbox", {
        name: "HTTPS listen addresses",
      });
      expect(https).toBeDisabled();
      expect(https).toHaveAccessibleDescription(
        /STARTSHIP_HTTPS_ADDR.*--https-listen/,
      );
      const user = userEvent.setup();
      const http = panel.getByRole("textbox", {
        name: "HTTP listen addresses",
      });
      await user.clear(http);
      await user.type(http, ":8888");
      await user.click(panel.getByRole("button", { name: "Save" }));
      expect(await panel.findByRole("alert")).toHaveTextContent(error.message);
      expect(http).toHaveValue(":8888");
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
    const http = await panel.findByRole("textbox", {
      name: "HTTP listen addresses",
    });
    const user = userEvent.setup();
    await user.clear(http);
    await user.type(http, ":8081");
    act(() => socket.receive({ type: "ready" }));
    const subscription = JSON.parse(socket!.send.mock.calls[0][0]);
    expect(subscription).toMatchObject({
      type: "subscribe",
      topic: "system_router",
    });
    act(() =>
      socket.receive({
        type: "snapshot",
        id: subscription.id,
        topic: "system_router",
        data: { ...config, enabled: false, https_listen: [":8443"] },
      }),
    );
    expect(await panel.findByText("Disabled")).toBeInTheDocument();
    expect(http).toHaveValue(":8081");
    expect(
      panel.getByRole("textbox", { name: "HTTPS listen addresses" }),
    ).toHaveValue(":8443");
    expect(
      vi
        .mocked(request)
        .mock.calls.filter(([path]) => path === "system/router"),
    ).toHaveLength(1);
  });
});
