import { describe, it, expect } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { applyLiveMessage } from "./live";
import { queries } from "./queries";
import {
  connectionEnv,
  orderedConnectionEnv,
  resourceDeployNeeded,
  validServiceName,
} from "./services";
import type { Project, Release, Service, ServiceEngine } from "./types";
describe("persistent resource deployment state", () => {
  const project = {
    id: "p",
    resources_updated: "2026-02-02T00:00:00Z",
    active_deployment: "old",
  } as Project;
  it("retains pending changes until the active release was created after them", () => {
    const releases = [
      { id: "old", created: "2026-02-01T00:00:00Z" },
      { id: "new", created: "2026-02-03T00:00:00Z" },
    ] as Release[];
    expect(resourceDeployNeeded(project, releases)).toBe(true);
    expect(
      resourceDeployNeeded({ ...project, active_deployment: "new" }, releases),
    ).toBe(false);
    expect(
      resourceDeployNeeded({ ...project, active_deployment: "" }, []),
    ).toBe(true);
    expect(
      resourceDeployNeeded(
        { ...project, resources_updated: undefined },
        releases,
      ),
    ).toBe(false);
    expect(resourceDeployNeeded(project, [])).toBe(false);
  });
});
describe("service and volume live snapshots", () => {
  it("replaces workspace snapshots, updates open details and isolates workspaces", () => {
    const client = new QueryClient(),
      services = [{ id: "s", name: "db", status: "running" }] as Service[];
    client.setQueryData(queries.services("other").queryKey, { services: [] });
    applyLiveMessage(
      client,
      { topic: "services", workspace_id: "w" },
      { id: "s", topic: "services", type: "snapshot", data: { services } },
    );
    expect(client.getQueryData(queries.services("w").queryKey)).toEqual({
      services,
    });
    expect(client.getQueryData(queries.service("s").queryKey)).toEqual(
      services[0],
    );
    expect(client.getQueryData(queries.services("other").queryKey)).toEqual({
      services: [],
    });
    const volumes = [{ id: "v", size_bytes: 2048 }];
    applyLiveMessage(
      client,
      { topic: "volumes", workspace_id: "w" },
      { id: "v", topic: "volumes", type: "snapshot", data: { volumes } },
    );
    expect(client.getQueryData(queries.volumes("w").queryKey)).toEqual({
      volumes,
    });
    applyLiveMessage(
      client,
      { topic: "services", workspace_id: "w" },
      { id: "s", topic: "services", type: "snapshot", data: { services: [] } },
    );
    expect(client.getQueryData(queries.services("w").queryKey)).toEqual({
      services: [],
    });
  });
  it("does not overwrite snapshots on errors", () => {
    const client = new QueryClient();
    client.setQueryData(queries.volumes("w").queryKey, { volumes: [] });
    applyLiveMessage(
      client,
      { topic: "volumes", workspace_id: "w" },
      { id: "v", topic: "volumes", type: "error", error: "Denied" },
    );
    expect(client.getQueryData(queries.volumes("w").queryKey)).toEqual({
      volumes: [],
    });
  });
});
describe("connection previews", () => {
  it("orders connection fields consistently, with extra variables last", () => {
    expect(
      orderedConnectionEnv({
        DB_URL: "url",
        DB_DATABASE: "db",
        DB_ZZZ: "z",
        DB_PASSWORD: "secret",
        DB_PORT: "5432",
        DB_USER: "app",
        DB_HOST: "db.local",
        DB_AAA: "a",
      }).map(([key]) => key),
    ).toEqual([
      "DB_HOST",
      "DB_PORT",
      "DB_USER",
      "DB_PASSWORD",
      "DB_DATABASE",
      "DB_URL",
      "DB_AAA",
      "DB_ZZZ",
    ]);
    expect(orderedConnectionEnv({})).toEqual([]);
    expect(
      orderedConnectionEnv({
        CACHE_URL: "url",
        CACHE_HOST: "cache.local",
        CACHE_PORT: "6379",
      }).map(([key]) => key),
    ).toEqual(["CACHE_HOST", "CACHE_PORT", "CACHE_URL"]);
  });
  it("matches backend environment names and connection URL", () => {
    const env = connectionEnv(
      {
        config: { user: "app", password: "secret", database: "app" },
      } as unknown as Service,
      { id: "postgresql", port: 5432 } as ServiceEngine,
      "main-db",
    );
    expect(env).toEqual({
      MAIN_DB_HOST: "main-db.local",
      MAIN_DB_PORT: "5432",
      MAIN_DB_USER: "app",
      MAIN_DB_PASSWORD: "secret",
      MAIN_DB_DATABASE: "app",
      MAIN_DB_URL: "postgres://app:secret@main-db.local:5432/app",
    });
  });
  it("supports credential-free engines and Mongo authentication", () => {
    expect(
      connectionEnv(
        { config: {} } as Service,
        { id: "redis", port: 6379 } as ServiceEngine,
        "cache",
      ),
    ).toEqual({
      CACHE_HOST: "cache.local",
      CACHE_PORT: "6379",
      CACHE_URL: "redis://cache.local:6379",
    });
    expect(
      connectionEnv(
        { config: {} } as Service,
        { id: "mongodb", port: 27017 } as ServiceEngine,
        "db",
      ).DB_URL,
    ).toContain("?authSource=admin");
  });
  it("rejects invalid aliases", () => {
    for (const value of ["", "UPPER", "space name", "-db", "a".repeat(64)])
      expect(validServiceName(value)).toBe(false);
    expect(validServiceName("app_db")).toBe(true);
  });
});
