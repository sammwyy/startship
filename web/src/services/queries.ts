import { queryOptions } from "@tanstack/react-query";
import { request } from "./api";
import type {
  AuditEntry,
  Container,
  DomainRoute,
  Permission,
  Project,
  Preview,
  Release,
  Role,
  User,
  Workspace,
  UpdateInfo,
  RouterConfig,
  SystemVersion,
  PipelineTemplate,
  ServiceEngine,
  Service,
  Volume,
  ProjectServices,
  Backups,
} from "./types";

function resource<T>(path: string) {
  return queryOptions({
    queryKey: [path],
    queryFn: ({ signal }) => request<T>(path, undefined, "GET", signal),
  });
}
export const queries = {
  catalog: () => resource<ServiceEngine[]>("catalog"),
  services: (id: string) =>
    resource<{ services: Service[] }>(`workspaces/${id}/services`),
  service: (id: string) => resource<Service>(`services/${id}`),
  backups: (id: string) => resource<Backups>(`workspaces/${id}/backups`),
  volumes: (id: string) =>
    resource<{ volumes: Volume[] }>(`workspaces/${id}/volumes`),
  projectServices: (id: string) =>
    resource<ProjectServices>(`projects/${id}/services`),
  pipelineTemplates: () => resource<PipelineTemplate[]>("pipeline/templates"),
  updates: () => ({
    ...resource<UpdateInfo>("system/update"),
    retry: false,
  }),
  router: () => resource<RouterConfig>("system/router"),
  version: () => ({
    ...resource<SystemVersion>("system/version"),
    retry: false,
  }),
  workspaces: () => resource<Workspace[]>("workspaces"),
  projects: (id: string) =>
    resource<Project[]>(`projects?workspace_id=${encodeURIComponent(id)}`),
  project: (id: string) => resource<Project>(`projects/${id}`),
  previews: (id: string) => resource<Preview[]>(`projects/${id}/previews`),
  containers: (id?: string) =>
    resource<Container[]>(`containers${id ? `?project_id=${id}` : ""}`),
  releases: (id?: string) =>
    resource<Release[]>(`releases?${id ? `project_id=${id}&` : ""}logs=false`),
  routes: (id: string) => resource<DomainRoute[]>(`routes?project_id=${id}`),
  roles: () => resource<Role[]>("roles"),
  permissions: () =>
    resource<{ permissions: Permission[]; icons: string[] }>("permissions"),
  users: () => resource<User[]>("users"),
  audit: (id?: string) =>
    resource<AuditEntry[]>(`audit${id ? `?workspace_id=${id}` : ""}`),
};
