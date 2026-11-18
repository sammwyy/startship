import { useQuery } from "@tanstack/react-query";
import { useLiveSubscription } from "../providers/LiveProvider";
import { queries } from "./queries";
import type { Service, ServiceEngine } from "./types";

export function useServices(workspaceId: string) {
  const liveError = useLiveSubscription({
    topic: "services",
    workspace_id: workspaceId,
  });
  return { ...useQuery(queries.services(workspaceId)), liveError };
}
export function useVolumes(workspaceId: string) {
  const liveError = useLiveSubscription({
    topic: "volumes",
    workspace_id: workspaceId,
  });
  return { ...useQuery(queries.volumes(workspaceId)), liveError };
}
export const validServiceName = (value: string) =>
  /^[a-z][a-z0-9_-]{0,62}$/.test(value);
export const envPrefix = (alias: string) =>
  alias.toUpperCase().replace(/[^A-Z0-9]/g, "_");
export function connectionEnv(
  service: Service,
  engine: ServiceEngine,
  alias: string,
) {
  const prefix = envPrefix(alias),
    host = `${alias}.local`,
    config = service.config;
  const scheme =
    (
      {
        postgresql: "postgres",
        mariadb: "mysql",
        rabbitmq: "amqp",
        valkey: "redis",
        clickhouse: "http",
        minio: "http",
        opensearch: "http",
      } as Record<string, string>
    )[engine.id] || engine.id;
  const escape = (value: string) =>
    encodeURIComponent(value).replace(
      /[!'()*]/g,
      (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`,
    );
  const auth = config.user
    ? `${escape(config.user)}:${escape(config.password || "")}@`
    : "";
  const database = config.database
    ? `/${config.database.split("/").map(escape).join("/")}`
    : "";
  const result: Record<string, string> = {
    [`${prefix}_HOST`]: host,
    [`${prefix}_PORT`]: String(engine.port),
    [`${prefix}_URL`]: `${scheme}://${auth}${host}:${engine.port}${database}${engine.id === "mongodb" ? "?authSource=admin" : ""}`,
  };
  for (const key of ["user", "password", "database"])
    if (config[key]) result[`${prefix}_${key.toUpperCase()}`] = config[key];
  return result;
}
export const externalEndpoint = (service: Service) =>
  service.external_port
    ? `${window.location.hostname}:${service.external_port}`
    : "Internal only";
export function secretValue(key: string) {
  return key.endsWith("_PASSWORD") || key.endsWith("_URL");
}
