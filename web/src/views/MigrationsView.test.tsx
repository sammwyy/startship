import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { request } from "../services/api";
import type {
  MigrationDetectedPlatform,
  MigrationImportRequest,
  MigrationJob,
} from "../services/types";
import { MigrationsView } from "./MigrationsView";

const session = vi.hoisted(() => ({ user: { platform_admin: true } }));
vi.mock("../providers/SessionProvider", () => ({ useSession: () => session }));
vi.mock("../providers/LiveProvider", () => ({
  useLiveSubscription: () => undefined,
}));
vi.mock("../services/api", async (original) => ({
  ...(await original<typeof import("../services/api")>()),
  request: vi.fn(),
}));

const platformsFixture: MigrationDetectedPlatform[] = [
  {
    platform: "openship",
    name: "Openship",
    detected: false,
    status: "not_found",
    projects: [],
    services: [],
  },
  {
    platform: "dokploy",
    name: "Dokploy",
    detected: true,
    status: "detected",
    data_dir: "/etc/dokploy",
    projects: [
      {
        id: "a1",
        platform: "dokploy",
        name: "store-api",
        source_kind: "git",
        git_url: "https://github.com/acme/store.git",
        build_type: "dockerfile",
        env_count: 3,
        log_count: 2,
        domains: [{ host: "api.store.test", https: true }],
        volumes: [
          {
            source_type: "volume",
            source_name: "store_data",
            mount_path: "/data",
          },
        ],
        compatible: true,
      },
      {
        id: "a2",
        platform: "dokploy",
        name: "store-web",
        source_kind: "git",
        git_url: "https://github.com/acme/store-web.git",
        env_count: 1,
        log_count: 0,
        compatible: true,
      },
    ],
    services: [
      {
        id: "pg1",
        platform: "dokploy",
        name: "store-db",
        engine: "postgresql",
        version: "16",
        compatible: true,
      },
    ],
  },
  {
    platform: "coolify",
    name: "Coolify",
    detected: false,
    status: "not_found",
    projects: [],
    services: [],
  },
];

let lastImport: MigrationImportRequest | undefined;

function renderView() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <MigrationsView />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  session.user.platform_admin = true;
  lastImport = undefined;
  vi.mocked(request).mockImplementation(async (path, body, method) => {
    if (path === "system/migrations/detect") {
      return { platforms: platformsFixture };
    }
    if (path === "system/migrations/jobs") {
      return { jobs: [] };
    }
    if (path === "workspaces") {
      return [{ id: "ws_1", name: "Production" }];
    }
    if (path === "system/migrations/import" && method === "POST") {
      lastImport = body as MigrationImportRequest;
      const job: MigrationJob = {
        id: "job_1",
        platform: lastImport.platform,
        mode: lastImport.mode,
        status: "completed",
        target_workspace_id: lastImport.target_workspace_id ?? "ws_new",
        target_workspace_name: lastImport.create_workspace_name ?? "Production",
        started_at: new Date().toISOString(),
        projects: [],
        services: [],
        logs: ["Migration finished with status: completed"],
      };
      return job;
    }
    throw new Error(`unexpected request: ${method} ${path}`);
  });
});

describe("MigrationsView", () => {
  it("restricts non-admins", () => {
    session.user.platform_admin = false;
    renderView();
    expect(screen.getByText("Platform Admin required")).toBeInTheDocument();
  });

  it("detects platforms and supports import-all into an existing workspace", async () => {
    const user = userEvent.setup();
    renderView();

    expect(await screen.findByText("Import from Dokploy")).toBeInTheDocument();
    expect(screen.getByText("store-api")).toBeInTheDocument();
    expect(screen.getByText("store-db")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Import all \(3\)/i }));

    await waitFor(() => {
      expect(lastImport).toMatchObject({
        platform: "dokploy",
        mode: "all",
        target_workspace_id: "ws_1",
        copy_volumes: true,
        import_domains: true,
        import_env: true,
        import_logs: true,
      });
    });
    expect(
      await screen.findByText("Migration finished with status: completed"),
    ).toBeInTheDocument();
  });

  it("supports cherry-picking a single project with a custom name into a new workspace", async () => {
    const user = userEvent.setup();
    renderView();

    expect(await screen.findByText("Import from Dokploy")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Cherry-pick" }));
    await user.click(screen.getByLabelText("Select project store-api"));
    await user.type(
      screen.getByLabelText("Target name for store-api"),
      "Migrated Store API",
    );

    await user.selectOptions(
      screen.getByLabelText("Target destination"),
      "new",
    );
    await user.type(
      screen.getByLabelText("New workspace name"),
      "Dokploy Copy",
    );

    await user.click(
      screen.getByRole("button", { name: /Import selected \(1\)/i }),
    );

    await waitFor(() => {
      expect(lastImport).toMatchObject({
        platform: "dokploy",
        mode: "cherry_pick",
        create_workspace_name: "Dokploy Copy",
        projects: [{ source_id: "a1", target_name: "Migrated Store API" }],
        services: [],
      });
    });
  });
});
