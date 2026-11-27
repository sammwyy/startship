export interface Scoped {
  permissions?: string[];
}
export interface ServiceField {
  key: string;
  label: string;
  type: "text" | "password" | "number";
  default: string;
  generate: boolean;
  required: boolean;
  help: string;
}
export interface ServiceEngine {
  id: string;
  icon: string;
  logo?: string;
  name: string;
  category: string;
  description: string;
  image: string;
  scheme?: string;
  versions: string[];
  port: number;
  data_path: string;
  fields: ServiceField[] | null;
}
export interface Service {
  id: string;
  workspace_id: string;
  name: string;
  engine: string;
  definition?: ServiceEngine;
  version: string;
  config: Record<string, string>;
  external_port: number;
  volume_id: string;
  backup?: BackupPolicy;
  next_backup?: string;
  attachments: { project_id: string; alias: string }[];
  status: string;
  last_error: string;
  container: string;
  created: string;
  updated: string;
  created_by: string;
}
export interface Volume {
  id: string;
  workspace_id: string;
  name: string;
  project_id: string;
  mount_path: string;
  service_id: string;
  purpose?: "backups";
  size_bytes: number;
  size_updated: string;
  created: string;
  created_by: string;
}
export interface BackupPolicy {
  enabled: boolean;
  frequency: "hourly" | "daily" | "weekly";
  minute: number;
  hour: number;
  weekday: number;
  keep: number;
}
export interface Backup {
  id: string;
  workspace_id: string;
  service_id: string;
  service_name: string;
  engine: string;
  version: string;
  volume_id: string;
  file: string;
  size_bytes: number;
  status: "running" | "completed" | "failed";
  trigger: "scheduled" | "manual" | "pre-restore";
  error?: string;
  created: string;
  finished: string;
  created_by: string;
}
export interface Backups {
  backups: Backup[];
  volume: Volume | null;
}
export interface ProjectServices {
  attached: { service: Service; alias: string; env: Record<string, string> }[];
  available: Service[];
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
  previews?: boolean;
  sleep_after?: number;
  sleeping?: boolean;
  uptime_monitoring?: boolean;
  resources_updated?: string;
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
}
export interface Container {
  pipeline?: Pipeline;
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
  preview_id?: string;
  branch?: string;
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
export interface Preview {
  id: string;
  project_id: string;
  branch: string;
  status: string;
  active_deployment?: string;
  sleeping?: boolean;
  commit?: string;
  addresses?: string[];
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
  display_name?: string;
  email?: string;
  session_id?: string;
  platform_admin?: boolean;
  must_change_password?: boolean;
  platform_roles?: string[];
}
export interface AccountSession {
  id: string;
  ip: string;
  user_agent: string;
  logged_at: string;
  last_activity_at: string;
  expires_at: string;
}
export interface AccessToken {
  id: string;
  name: string;
  prefix: string;
  all_workspaces: boolean;
  workspaces: string[];
  access: "operate" | "admin";
  api: boolean;
  mcp: boolean;
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
  last_used_ip?: string;
  last_user_agent?: string;
}
export interface ActivityEntry {
  id: number;
  at: string;
  actor: string;
  actor_id: string;
  action: string;
  target: string;
  scope: string;
  scope_id: string;
  source?: string;
  session_id?: string;
  token_id?: string;
  token?: string;
  detail?: unknown;
}
export interface SystemVersion {
  version: string;
  commit: string;
  unavailable_engines?: Record<string, string>;
}
export interface RouterSettings {
  enabled: boolean;
  http_listen: string[];
  https_listen: string[];
}
export type RouterOverride = "router_disabled" | "http_listen" | "https_listen";
export interface RouterConfig extends RouterSettings {
  config_path: string;
  overridden: RouterOverride[];
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

export type MigrationPlatform = "openship" | "dokploy" | "coolify";

export interface MigrationDiscoveredDomain {
  host: string;
  https?: boolean;
  path_prefix?: string;
  strip_prefix?: boolean;
  port?: number;
}

export interface MigrationDiscoveredVolume {
  source_type: string;
  source_name: string;
  mount_path: string;
  read_only?: boolean;
}

export interface MigrationDiscoveredProject {
  id: string;
  platform: MigrationPlatform;
  source_org?: string;
  source_project?: string;
  environment?: string;
  name: string;
  description?: string;
  source_kind: string;
  git_url?: string;
  branch?: string;
  root_dir?: string;
  build_type?: string;
  dockerfile?: string;
  docker_image?: string;
  port?: number;
  env_keys?: string[];
  env_count: number;
  domains?: MigrationDiscoveredDomain[];
  volumes?: MigrationDiscoveredVolume[];
  log_count: number;
  linked_services?: string[];
  compatible: boolean;
  warnings?: string[];
}

export interface MigrationDiscoveredService {
  id: string;
  platform: MigrationPlatform;
  source_org?: string;
  source_project?: string;
  environment?: string;
  name: string;
  engine?: string;
  raw_engine?: string;
  image?: string;
  version?: string;
  external_port?: number;
  config_keys?: string[];
  volumes?: MigrationDiscoveredVolume[];
  compatible: boolean;
  warnings?: string[];
}

export interface MigrationDetectedPlatform {
  platform: MigrationPlatform;
  name: string;
  detected: boolean;
  status: string;
  data_dir?: string;
  db_container?: string;
  error?: string;
  warnings?: string[];
  projects: MigrationDiscoveredProject[];
  services: MigrationDiscoveredService[];
}

export interface MigrationProjectSelection {
  source_id: string;
  target_workspace_id?: string;
  target_name?: string;
}

export interface MigrationServiceSelection {
  source_id: string;
  target_workspace_id?: string;
  target_name?: string;
}

export interface MigrationImportRequest {
  platform: MigrationPlatform;
  mode: "all" | "cherry_pick";
  target_workspace_id?: string;
  create_workspace_name?: string;
  copy_volumes?: boolean;
  import_domains?: boolean;
  import_env?: boolean;
  import_logs?: boolean;
  start_services?: boolean;
  deploy_projects?: boolean;
  projects?: MigrationProjectSelection[];
  services?: MigrationServiceSelection[];
}

export interface MigrationProjectResult {
  source_id: string;
  source_name: string;
  project_id?: string;
  project_name?: string;
  workspace_id?: string;
  volumes_copied: number;
  domains_imported: number;
  env_count: number;
  logs_imported: number;
  deployment_id?: string;
  status: string;
  warnings?: string[];
  error?: string;
}

export interface MigrationServiceResult {
  source_id: string;
  source_name: string;
  service_id?: string;
  service_name?: string;
  workspace_id?: string;
  engine?: string;
  volumes_copied: number;
  attached_projects: number;
  status: string;
  warnings?: string[];
  error?: string;
}

export interface MigrationJob {
  id: string;
  platform: MigrationPlatform;
  mode: string;
  status: string;
  target_workspace_id?: string;
  target_workspace_name?: string;
  created_by?: string;
  started_at: string;
  finished_at?: string;
  projects: MigrationProjectResult[];
  services: MigrationServiceResult[];
  logs: string[];
  warnings?: string[];
  error?: string;
}
