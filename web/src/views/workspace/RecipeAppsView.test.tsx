import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeAll, expect, it, vi } from "vitest";
import { UIProvider } from "../../providers/UIProvider";
import { request } from "../../services/api";
import type { Recipe, RecipeApps } from "../../services/recipeApps";
import { AppsMarketplaceView, RecipeAppsView } from "./RecipeAppsView";

vi.mock("../../layouts/DashboardLayout", () => ({
  useDashboard: () => ({ workspace: { id: "w" } }),
}));
vi.mock("../../providers/SessionProvider", () => ({
  useSession: () => ({
    user: { platform_admin: true },
    can: () => true,
  }),
}));
vi.mock("../../providers/LiveProvider", () => ({
  useLiveSubscription: () => undefined,
}));
vi.mock("../../services/api", () => ({ request: vi.fn(), remove: vi.fn() }));

beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
});

const jellyfin: Recipe = {
  id: "jellyfin",
  name: "Jellyfin",
  icon: "film",
  logo: "jellyfin",
  category: "media",
  description: "Your personal media library.",
  version: "1",
  config_schema: { properties: {} },
  containers: [{ id: "web", image: "jellyfin/jellyfin" }],
  volumes: null,
  http: null,
  ports: [
    { id: "web", container: "web", port: 80, protocol: "tcp", host_port: 8096 },
  ],
  files: null,
};
const installed: RecipeApps = {
  can_manage: true,
  apps: [
    {
      id: "media",
      name: "Home media",
      recipe: jellyfin,
      status: "running",
      running: true,
      config: {},
      ports: { web: 9000 },
      domains: [{ id: "host", host: "media.example.com", endpoint: "web" }],
      containers: {},
    },
  ],
};

function renderApps() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <UIProvider>
        <MemoryRouter initialEntries={["/workspace/w/apps"]}>
          <Routes>
            <Route
              path="/workspace/:workspaceId/apps/:appId?"
              element={<RecipeAppsView />}
            />
            <Route
              path="/workspace/:workspaceId/apps/marketplace/:entryId?"
              element={<AppsMarketplaceView />}
            />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </QueryClientProvider>,
  );
}

it("switches layouts and restores the browser preference on a new mount", async () => {
  const user = userEvent.setup();
  vi.mocked(request).mockImplementation(async (path) =>
    path === "recipes" ? [jellyfin] : installed,
  );
  const { unmount } = renderApps();
  expect(
    await screen.findByRole("link", { name: "Home media" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Grid" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await user.click(screen.getByRole("button", { name: "Cards" }));
  expect(screen.getByText("media.example.com · 9000/tcp")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "List" }));
  unmount();
  renderApps();
  expect(await screen.findByRole("button", { name: "List" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  localStorage.removeItem("startship:apps:layout");
});

it("browses the routed marketplace by category and starts installation from app details", async () => {
  const user = userEvent.setup();
  vi.mocked(request).mockImplementation(async (path) => {
    if (path === "recipes/status") return { source: "bundled" };
    if (path === "recipes")
      return [
        jellyfin,
        { ...jellyfin, id: "gitea", name: "Gitea", category: "dev-tools" },
      ];
    return installed;
  });
  renderApps();
  await user.click(await screen.findByRole("link", { name: "Install app" }));
  expect(
    await screen.findByRole("heading", { name: "App marketplace" }),
  ).toBeInTheDocument();
  await user.click(await screen.findByRole("button", { name: "Media" }));
  expect(screen.queryByRole("link", { name: /Gitea/ })).not.toBeInTheDocument();
  await user.type(
    screen.getByRole("textbox", { name: "Search app catalog" }),
    "jelly",
  );
  await user.click(screen.getByRole("link", { name: /Jellyfin/ }));
  expect(
    await screen.findByRole("heading", { name: "Jellyfin" }),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Install app" }));
  expect(screen.getByLabelText("App name")).toHaveValue("jellyfin");
});

it.each([
  { status: "exited", running: false, badge: "exited" },
  { status: "running", running: true, badge: "unhealthy" },
])(
  "shows $badge for a $status container with unhealthy health",
  async ({ status, running, badge }) => {
    vi.mocked(request).mockResolvedValue({
      can_manage: false,
      apps: [
        {
          id: "app",
          name: "Status page",
          recipe: {
            id: "status-page",
            name: "Status Page",
            description: "",
            icon: "box",
            version: "1",
            config_schema: { properties: {} },
            containers: [{ id: "web", image: "busybox" }],
            volumes: null,
            http: null,
            ports: null,
            files: null,
          },
          status: running ? "degraded" : "stopped",
          running,
          config: null,
          ports: {},
          domains: [],
          containers: { web: { status, health: "unhealthy" } },
        },
      ],
    } satisfies RecipeApps);

    const client = new QueryClient();
    render(
      <QueryClientProvider client={client}>
        <UIProvider>
          <MemoryRouter initialEntries={["/workspace/w/apps/app"]}>
            <Routes>
              <Route
                path="/workspace/:workspaceId/apps/:appId"
                element={<RecipeAppsView />}
              />
            </Routes>
          </MemoryRouter>
        </UIProvider>
      </QueryClientProvider>,
    );

    expect(await screen.findByText(badge)).toBeInTheDocument();
  },
);

it("generates a certificate inline when connecting a domain to an app", async () => {
  const user = userEvent.setup();
  const appWithHttp: RecipeApps = {
    can_manage: true,
    apps: [
      {
        ...installed.apps[0],
        domains: [],
        recipe: {
          ...jellyfin,
          http: [{ id: "web", container: "web", port: 80 }],
        },
      },
    ],
  };
  vi.mocked(request).mockImplementation(async (path, body) => {
    if (path === "workspaces/w/apps") return appWithHttp;
    if (path === "workspaces/w/domains")
      return {
        domains: [
          {
            id: "d1",
            name: "example.com",
            apex: true,
            subdomains: true,
            routes: 0,
          },
        ],
      };
    if (path === "workspaces/w/certificates" && !body)
      return { certificates: [] };
    if (path === "workspaces/w/certificates" && body)
      return {
        id: "cert-inline",
        workspace_id: "w",
        domains: ["example.com"],
        source: "acme",
        issuer: "",
        not_before: "",
        not_after: "",
        status: "pending",
        auto_renew: true,
      };
    return {};
  });

  render(
    <QueryClientProvider client={new QueryClient()}>
      <UIProvider>
        <MemoryRouter initialEntries={["/workspace/w/apps/media"]}>
          <Routes>
            <Route
              path="/workspace/:workspaceId/apps/:appId"
              element={<RecipeAppsView />}
            />
          </Routes>
        </MemoryRouter>
      </UIProvider>
    </QueryClientProvider>,
  );

  await user.click(
    await screen.findByRole("button", { name: "Connect domain" }),
  );
  const generate = await screen.findByRole("option", {
    name: /Generate certificate for example\.com/,
  });
  await user.click(generate);
  expect(request).toHaveBeenCalledWith("workspaces/w/certificates", {
    source: "acme",
    domains: ["example.com"],
    auto_renew: true,
  });
  await user.click(screen.getByRole("button", { name: "Connect" }));
  expect(request).toHaveBeenCalledWith("workspaces/w/apps/media/domains", {
    endpoint: "web",
    host: "example.com",
    certificate_id: "cert-inline",
    force_https: false,
  });
});
