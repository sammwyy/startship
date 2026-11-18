import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UIProvider } from "../../providers/UIProvider";
import { request, remove } from "../../services/api";
import { applyLiveMessage } from "../../services/live";
import { useLiveSubscription } from "../../providers/LiveProvider";
import { durationSeconds, durationText } from "../../services/firewall";
import type { Catalog, FirewallSnapshot } from "../../services/firewall";
import { FirewallView } from "./FirewallView";
import { NetworkFirewallView } from "../NetworkFirewallView";

let allowed = true;
vi.mock("../../providers/SessionProvider", () => ({
  useSession: () => ({ user: { platform_admin: allowed }, can: () => allowed }),
}));
vi.mock("../../layouts/DashboardLayout", () => ({
  useDashboard: () => ({ workspace: { id: "w" } }),
}));
vi.mock("../../providers/LiveProvider", () => ({
  useLiveSubscription: vi.fn(() => null),
  useLiveStatus: () => "connected",
}));
vi.mock("../../services/api", () => ({ request: vi.fn(), remove: vi.fn() }));

const catalog: Catalog = {
  geoip: false,
  actions: ["block", "allow", "log", "rate_limit", "ban"],
  fields: [
    { id: "method", ops: ["eq", "in"] },
    { id: "path", ops: ["eq", "in", "prefix", "matches", "exists"] },
    { id: "country", ops: ["eq", "in"] },
    {
      id: "ua_class",
      ops: ["eq", "in"],
      values: ["browser", "crawler", "bot"],
    },
    { id: "user_agent", ops: ["eq", "matches"] },
  ],
  managed_rules: [
    {
      id: "sqli",
      name: "SQL injection",
      description: "SQL signatures",
      default_action: "block",
      default_enabled: true,
    },
  ],
};
const snapshot: FirewallSnapshot = {
  policy: {
    mode: "monitor",
    managed: { sqli: { enabled: true, action: "block" } },
    rules: [],
    limits: {
      max_body_bytes: 0,
      max_url_bytes: 8192,
      max_header_bytes: 0,
      concurrent_per_ip: 0,
      rate: null,
    },
    updated: "",
    updated_by: "",
  },
  lists: [{ id: "banned", name: "Banned", kind: "block", entries: [] }],
  events: [
    {
      time: "2026-11-18T12:00:00Z",
      workspace_id: "w",
      project_id: "p",
      route_host: "app.test",
      ip: "203.0.113.5",
      country: "US",
      method: "GET",
      host: "app.test",
      path: "/login",
      ua: "sqlmap",
      ua_class: "bot",
      rule_id: "sqli",
      rule_name: "SQL injection",
      action: "block",
      status: 403,
      monitored: false,
    },
  ],
  stats: {
    total: 120,
    blocked: 12,
    by_action: { block: 12 },
    minutes: [
      { at: 1795003200, total: 120, blocked: 12, by_action: { block: 12 } },
    ],
    top_rules: [{ value: "SQL injection", count: 12 }],
    top_ips: [],
    top_countries: [],
  },
  can_manage: true,
};
beforeEach(() => {
  vi.clearAllMocks();
  allowed = true;
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute("open");
  };
  HTMLElement.prototype.hidePopover = vi.fn();
  HTMLElement.prototype.showPopover = vi.fn();
  HTMLElement.prototype.scrollIntoView = vi.fn();
  vi.mocked(request).mockImplementation(async (path) =>
    path === "firewall/catalog"
      ? catalog
      : path === "system/network-firewall"
        ? { enabled: false, available: false, rules: [] }
        : {},
  );
  vi.mocked(remove).mockResolvedValue(undefined);
});
function mount(
  section = "overview",
  data: FirewallSnapshot | undefined = structuredClone(snapshot),
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  if (data) client.setQueryData(["workspaces/w/firewall"], data);
  const result = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/workspace/w/firewall/${section}`]}>
        <UIProvider>
          <Routes>
            <Route
              path="workspace/:workspaceId/firewall/:section?"
              element={<FirewallView />}
            />
          </Routes>
        </UIProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...result, client };
}
async function pick(label: string, value: string) {
  const trigger = screen.getByRole("button", { name: label });
  await userEvent.click(trigger);
  const menu = document.getElementById(trigger.getAttribute("popovertarget")!)!;
  await userEvent.click(within(menu).getByRole("option", { name: value }));
}
describe("workspace firewall", () => {
  it("renders live snapshots, subscribes without a policy GET, and replaces the cache", async () => {
    const { container, client } = mount();
    expect(screen.getByText("203.0.113.5")).toBeInTheDocument();
    expect(screen.getByText("10.0%")).toBeInTheDocument();
    expect(
      screen.getByRole("img", { name: /Allowed and blocked/ }),
    ).toBeInTheDocument();
    expect(container.querySelector("select")).toBeNull();
    expect(useLiveSubscription).toHaveBeenCalledWith({
      topic: "firewall",
      workspace_id: "w",
    });
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("firewall/catalog"),
    );
    expect(
      vi
        .mocked(request)
        .mock.calls.every(([path]) => path === "firewall/catalog"),
    ).toBe(true);
    act(() =>
      applyLiveMessage(
        client,
        { topic: "firewall", workspace_id: "w" },
        {
          id: "fw-test",
          type: "snapshot",
          topic: "firewall",
          data: { ...snapshot, events: [] },
        },
      ),
    );
    expect(
      await screen.findByText("No firewall events yet"),
    ).toBeInTheDocument();
    expect(screen.queryByText("203.0.113.5")).not.toBeInTheDocument();
  });
  it("PUTs the complete policy when changing mode and surfaces API failures", async () => {
    mount();
    await userEvent.click(screen.getByRole("button", { name: "Enforce" }));
    expect(request).toHaveBeenCalledWith(
      "workspaces/w/firewall",
      { ...snapshot.policy, mode: "enforce" },
      "PUT",
    );
    vi.mocked(request).mockRejectedValueOnce(new Error("Policy update failed"));
    await userEvent.click(screen.getByRole("button", { name: "Off" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Policy update failed",
    );
  });
  it("resolves the top rule ID to its name", () => {
    const data = structuredClone(snapshot);
    data.stats.top_rules = [{ value: "custom-id", count: 12 }];
    data.policy.rules = [
      {
        id: "custom-id",
        name: "Protect login endpoint",
        enabled: true,
        match: "all",
        conditions: [{ field: "path", op: "prefix", value: "/login" }],
        action: "block",
      },
    ];
    mount("overview", data);
    expect(screen.getByTitle("Protect login endpoint")).toBeInTheDocument();
    expect(screen.queryByText("custom-id")).not.toBeInTheDocument();
  });
  it("disables mutations for a read-only viewer and denies missing permission", async () => {
    const view = mount("overview", { ...snapshot, can_manage: false });
    expect(screen.getByRole("button", { name: "Enforce" })).toBeDisabled();
    expect(screen.getByText(/Read only/)).toBeInTheDocument();
    view.unmount();
    allowed = false;
    mount();
    expect(screen.getByText("Firewall access required")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Enforce" }),
    ).not.toBeInTheDocument();
    expect(useLiveSubscription).toHaveBeenLastCalledWith(null);
  });
  it("creates the login preset with the exact condition, rate and ban JSON", async () => {
    const { container } = mount("rules");
    await userEvent.click(
      await screen.findByRole("button", { name: "Protect login endpoint" }),
    );
    expect(container.querySelector("select")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Save rule" }));
    expect(request).toHaveBeenCalledWith(
      "workspaces/w/firewall/rules",
      {
        id: "",
        name: "Protect login endpoint",
        enabled: true,
        match: "all",
        conditions: [
          { field: "method", op: "eq", value: "POST" },
          { field: "path", op: "prefix", value: "/login" },
        ],
        action: "rate_limit",
        rate: { requests: 10, window_seconds: 60, by: "ip" },
        ban_seconds: 900,
      },
      "POST",
    );
  });
  it("builds country chips, filters operators by field, warns about GeoIP and reports invalid rules", async () => {
    mount("rules");
    await userEvent.click(
      await screen.findByRole("button", { name: "Block countries" }),
    );
    expect(screen.getByText(/GeoIP database unavailable/)).toBeInTheDocument();
    const op = screen.getByRole("listbox", { name: "Condition 1 operator" });
    expect(
      within(op)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["eq", "in"]);
    await userEvent.type(
      screen.getByRole("textbox", { name: "Condition values" }),
      "US,CA{Enter}",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Any condition" }),
    );
    vi.mocked(request).mockRejectedValueOnce(
      new Error("rules[0]: invalid country"),
    );
    await userEvent.click(screen.getByRole("button", { name: "Save rule" }));
    expect(request).toHaveBeenLastCalledWith(
      "workspaces/w/firewall/rules",
      expect.objectContaining({
        match: "any",
        conditions: [{ field: "country", op: "in", value: ["US", "CA"] }],
      }),
      "POST",
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "rules[0]: invalid country",
    );
  });
  it("uses enum pickers and creates a custom rule from the condition builder", async () => {
    mount("rules");
    await userEvent.click(
      await screen.findByRole("button", { name: "Add rule" }),
    );
    await userEvent.type(screen.getByLabelText("Rule name"), "Bots");
    await pick("Condition 1 field", "ua class");
    await pick("Condition 1 value", "bot");
    await pick("Rule action", "log");
    await userEvent.click(screen.getByRole("button", { name: "Save rule" }));
    expect(request).toHaveBeenLastCalledWith(
      "workspaces/w/firewall/rules",
      expect.objectContaining({
        name: "Bots",
        action: "log",
        conditions: [{ field: "ua_class", op: "eq", value: "bot" }],
      }),
      "POST",
    );
  });
  it("saves limits using byte units and duration parsing", async () => {
    mount("limits");
    await userEvent.clear(screen.getByLabelText("Requests"));
    await userEvent.type(screen.getByLabelText("Requests"), "25");
    await pick("Max body unit", "MB");
    await userEvent.clear(screen.getByLabelText("Max body"));
    await userEvent.type(screen.getByLabelText("Max body"), "10");
    await userEvent.click(screen.getByRole("button", { name: "Save limits" }));
    expect(request).toHaveBeenLastCalledWith(
      "workspaces/w/firewall",
      expect.objectContaining({
        limits: expect.objectContaining({
          max_body_bytes: 10000000,
          rate: {
            requests: 25,
            window_seconds: 60,
            action: "block",
            ban_seconds: 0,
          },
        }),
      }),
      "PUT",
    );
  });
  it("renders empty bans and adds an expiring list entry", async () => {
    mount("lists");
    expect(screen.getByText(/No entries/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Add entry" }));
    await userEvent.type(
      screen.getByLabelText("IP address or CIDR"),
      "203.0.113.8",
    );
    await userEvent.type(
      screen.getByLabelText("Expires after (optional)"),
      "15m",
    );
    await userEvent.click(
      screen.getAllByRole("button", { name: "Add entry" })[1],
    );
    expect(request).toHaveBeenLastCalledWith(
      "workspaces/w/firewall/lists/banned/entries",
      { value: "203.0.113.8", ttl_seconds: 900, note: "" },
    );
  });
});
describe("network firewall", () => {
  function network() {
    return render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter>
          <UIProvider>
            <NetworkFirewallView />
          </UIProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  }
  it("explains the disabled state, requirements and installer configuration", async () => {
    const { container } = network();
    expect(
      await screen.findByText("Network firewall is disabled"),
    ).toBeInTheDocument();
    expect(screen.getByText("network_firewall = true")).toBeInTheDocument();
    expect(
      screen.getByText("/etc/startship/startship.toml"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("STARTSHIP_NETWORK_FIREWALL=1"),
    ).toBeInTheDocument();
    expect(screen.getByText(/sh install.sh/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add rule" })).toBeDisabled();
    expect(container.querySelector("select")).toBeNull();
  });
  it("shows enabled rules, errors and expiry", async () => {
    vi.mocked(request).mockResolvedValue({
      enabled: true,
      available: true,
      error: "ipset reconciliation failed",
      rules: [
        {
          id: "n",
          kind: "cidr",
          value: "203.0.113.0/24",
          ports: "router",
          note: "Abuse",
          expires_at: "2099-01-01T00:00:00Z",
        },
      ],
    });
    network();
    expect(await screen.findByText("203.0.113.0/24")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "ipset reconciliation failed",
    );
    expect(screen.getByText("Router only")).toBeInTheDocument();
  });
  it("surfaces a network status request failure", async () => {
    vi.mocked(request).mockRejectedValue(new Error("Status unavailable"));
    network();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Status unavailable",
    );
  });
});
describe("firewall durations", () => {
  it("supports the CLI duration units, empty and invalid inputs", () => {
    expect(["30s", "15m", "1h", "7d", "", "0"].map(durationSeconds)).toEqual([
      30, 900, 3600, 604800, 0, 0,
    ]);
    expect(durationText(900)).toBe("15m");
    expect(() => durationSeconds("tomorrow")).toThrow(/whole-second duration/);
    expect(() => durationSeconds("0.1s")).toThrow();
  });
});
