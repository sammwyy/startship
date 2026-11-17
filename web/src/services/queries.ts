import { queryOptions } from "@tanstack/react-query";
import { request } from "./api";
import type {
  AuditEntry,
  Container,
  DomainRoute,
  Permission,
  Project,
  Release,
  Role,
  User,
  Workspace,
} from "./types";

function resource<T>(path: string) {
  return queryOptions({
    queryKey: [path],
    queryFn: ({ signal }) => request<T>(path, undefined, "GET", signal),
  });
}
export const queries = {
  workspaces: () => resource<Workspace[]>("workspaces"),
  projects: (id: string) =>
    resource<Project[]>(`projects?workspace_id=${encodeURIComponent(id)}`),
  project: (id: string) => resource<Project>(`projects/${id}`),
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
