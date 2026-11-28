import { useQuery } from "@tanstack/react-query";
import { useLiveSubscription } from "../providers/LiveProvider";
import { queries } from "./queries";
import type {
  Endpoint,
  EndpointKind,
  Project,
  Release,
  Service,
  ServiceEngine,
} from "./types";

export function resourceDeployNeeded(project: Project, releases: Release[]) {
  if (!project.resources_updated) return false;
  if (!project.active_deployment) return true;
  const active = releases.find((r) => r.id === project.active_deployment);
  return (
    !!active &&
    new Date(project.resources_updated).getTime() >
      new Date(active.created).getTime()
  );
}
export function useResourceDeployNeeded(project?: Project) {
  const releases = useQuery({
    ...queries.releases(project?.id),
    enabled: !!project?.active_deployment && !!project.resources_updated,
  });
  return !!project && resourceDeployNeeded(project, releases.data || []);
}

export function useServices(workspaceId: string) {
  const liveError = useLiveSubscription({
    topic: "services",
    workspace_id: workspaceId,
  });
  return { ...useQuery(queries.services(workspaceId)), liveError };
}
export function useConnections(workspaceId: string) {
  const liveError = useLiveSubscription({
    topic: "connections",
    workspace_id: workspaceId,
  });
  return { ...useQuery(queries.connections(workspaceId)), liveError };
}
export function useNetworks(workspaceId: string) {
  const liveError = useLiveSubscription({
    topic: "networks",
    workspace_id: workspaceId,
  });
  return { ...useQuery(queries.networks(workspaceId)), liveError };
}
export const sameEndpoint = (a: Endpoint, b: Endpoint) =>
  a.kind === b.kind && a.id === b.id;
export const endpointHost = (kind: EndpointKind, name: string) =>
  kind === "service"
    ? name
    : name
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "");
export function useVolumes(workspaceId: string) {
  const liveError = useLiveSubscription({
    topic: "volumes",
    workspace_id: workspaceId,
  });
  return { ...useQuery(queries.volumes(workspaceId)), liveError };
}
export function useBackups(workspaceId: string) {
  const liveError = useLiveSubscription({
    topic: "backups",
    workspace_id: workspaceId,
  });
  return { ...useQuery(queries.backups(workspaceId)), liveError };
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
    engine.scheme ||
    (
      {
        postgresql: "postgres",
        mariadb: "mysql",
        rabbitmq: "amqp",
        valkey: "redis",
        clickhouse: "http",
        seaweedfs: "http",
        opensearch: "https",
      } as Record<string, string>
    )[engine.id] ||
    engine.id;
  const escape = (value: string) =>
    encodeURIComponent(value).replace(
      /[!'()*]/g,
      (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`,
    );
  const auth =
    config.user || config.password
      ? `${escape(config.user || "")}:${escape(config.password || "")}@`
      : "";
  const database = config.database
    ? `/${config.database.split("/").map(escape).join("/")}`
    : "";
  const result: Record<string, string> = {
    [`${prefix}_HOST`]: host,
    [`${prefix}_PORT`]: String(engine.port),
    [`${prefix}_URL`]: `${scheme}://${auth}${host}:${engine.port}${database}${engine.id === "mongodb" ? "?authSource=admin" : ""}`,
  };
  for (const key of [
    "user",
    "password",
    "database",
    "access_key",
    "secret_key",
  ])
    if (config[key]) result[`${prefix}_${key.toUpperCase()}`] = config[key];
  if (engine.id === "seaweedfs" || engine.id === "minio")
    result[`${prefix}_ENDPOINT`] = result[`${prefix}_URL`];
  return Object.fromEntries(orderedConnectionEnv(result));
}
export const externalEndpoint = (service: Service) =>
  service.external_port
    ? `${window.location.hostname}:${service.external_port}`
    : "Internal only";
export function secretValue(key: string) {
  return (
    key.endsWith("_PASSWORD") ||
    key.endsWith("_URL") ||
    key.endsWith("_SECRET_KEY")
  );
}
export function orderedConnectionEnv(env: Record<string, string>) {
  const fields = [
    "HOST",
    "PORT",
    "USER",
    "PASSWORD",
    "DATABASE",
    "URL",
    "ACCESS_KEY",
    "SECRET_KEY",
    "ENDPOINT",
  ];
  const rank = (key: string) => {
    const index = fields.findIndex((field) => key.endsWith("_" + field));
    return index < 0 ? fields.length : index;
  };
  return Object.entries(env).sort(
    ([a], [b]) => rank(a) - rank(b) || a.localeCompare(b),
  );
}
