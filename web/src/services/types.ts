export interface Scoped {
  permissions?: string[];
}
export interface Pipeline {
  build: {
    engine?: "docker" | "nixpacks" | "buildpacks";
    image?: string;
    commands?: string[];
    dockerfile?: string;
  };
  test: {
    image?: string;
    commands?: string[];
    expect_exit_code: number;
    expect_output?: string;
  };
  deploy: {
    image?: string;
    artifact?: string;
    static?: boolean;
    command?: string;
    port?: number;
    health_path?: string;
    expect_status?: number;
    health_timeout?: number;
  };
}
export interface PipelineTemplate {
  id: string;
  name: string;
  description: string;
  icon: string;
  category: string;
  pipeline: Pipeline;
}
export interface DetectedPipeline {
  root_dir: string;
  build_type: string;
  port: number;
  template: string;
  pipeline: Pipeline;
}
export interface Workspace extends Scoped {
  id: string;
  name: string;
  created?: string;
}
export interface Project extends Workspace {
  workspace_id: string;
  git_url?: string;
  branch?: string;
  has_git_token?: boolean;
  webhook_secret?: string;
  root_dir?: string;
  build_type?: string;
  pipeline?: Pipeline;
  port?: number;
  test_command?: string;
  auto_deploy?: boolean;
  env?: Record<string, string>;
  active_deployment?: string;
  config_conflict?: boolean;
  source_conflict?: boolean;
}
export interface Container {
  id: string;
  name: string;
  project_id: string;
  root_dir?: string;
  build_type?: string;
  port: number;
  status?: string;
  active_deployment?: string;
  release_id?: string;
}
export interface Release {
  id: string;
  project_id: string;
  status: string;
  created: string;
  commit?: string;
  container_releases?: string[];
  redeploy_of?: string;
  redeployable?: boolean;
  log?: string;
}
export interface DomainRoute {
  id: string;
  host: string;
  path_prefix: string;
  strip_prefix: boolean;
  port?: number;
  container_id?: string;
  app_id?: string;
  url?: string;
  certificate_id?: string;
  force_https?: boolean;
}
export interface Certificate {
  id: string;
  workspace_id: string;
  domains: string[];
  source: "upload" | "acme";
  cert_pem?: string;
  issuer: string;
  not_before: string;
  not_after: string;
  status: "pending" | "issuing" | "active" | "failed" | "expired";
  last_error?: string;
  auto_renew: boolean;
}
export interface ACMESettings {
  email: string;
  directory_url: string;
}
export interface WorkspaceDomain {
  id: string;
  name: string;
  apex: boolean;
  subdomains: boolean;
  routes: number;
}
export interface User extends Scoped {
  id: string;
  username: string;
  platform_admin?: boolean;
  must_change_password?: boolean;
  platform_roles?: string[];
}
export interface SystemVersion {
  version: string;
  commit: string;
}
export interface UpdateRequest {
  version: string;
  by?: string;
  requested?: string;
}
export interface UpdateInfo {
  current: string;
  commit: string;
  latest?: string;
  update_available: boolean;
  development: boolean;
  release_url?: string;
  notes?: string;
  checked_at?: string;
  check_error?: string;
  self_update: boolean;
  pending?: UpdateRequest;
  update?: {
    state: "running" | "succeeded" | "failed";
    version: string;
    error?: string;
    started?: string;
    finished?: string;
  };
}
export interface Role {
  id: string;
  name: string;
  color: string;
  icon: string;
  permissions: string[];
  builtin: boolean;
  fixed?: boolean;
  members: number;
}
export interface Permission {
  key: string;
  label: string;
  description: string;
  group: string;
  scope: string;
}
export interface Assignment {
  id: string;
  user_id: string;
  role_id: string;
  scope: string;
  scope_id: string;
  project_name?: string;
}
export interface Member {
  user_id: string;
  username: string;
  roles: Assignment[];
}
export interface Override {
  scope: string;
  scope_id: string;
  subject_type: string;
  subject_id: string;
  allow: string[];
  deny: string[];
}
export interface AuditEntry {
  id?: string;
  at: string;
  actor: string;
  action: string;
  target: string;
  scope: string;
}
export interface TrafficBucket {
  at: number;
  requests: number;
  errors: number;
  average_ms: number;
  cpu_percent: number | null;
  memory_bytes: number | null;
}
export interface Traffic {
  total: {
    requests: number;
    errors: number;
    client_errors: number;
    average_ms: number;
    max_ms: number;
  };
  containers: {
    name: string;
    cpu_percent: number;
    memory_bytes: number;
    limit_bytes: number;
  }[];
  resource_status: string;
  sampled_at: string;
  interval_seconds: number;
  buckets: TrafficBucket[];
  recent: {
    at: string;
    method: string;
    route: string;
    status: number;
    duration_ms: number;
  }[];
}
