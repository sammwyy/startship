import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ErrorMessage, Loading } from "../components/ui";
import { Icon } from "../components/Icon";
import { useLiveSubscription } from "../providers/LiveProvider";
import { useSession } from "../providers/SessionProvider";
import { request } from "../services/api";
import { queries } from "../services/queries";
import type {
  MigrationDetectedPlatform,
  MigrationImportRequest,
  MigrationJob,
  MigrationPlatform,
} from "../services/types";
import "./MigrationsView.css";

function statusTone(status: string): string {
  switch (status) {
    case "detected":
    case "completed":
    case "ok":
      return "ok";
    case "running":
      return "info";
    case "completed_with_warnings":
    case "warning":
      return "warn";
    case "failed":
    case "error":
      return "error";
    default:
      return "muted";
  }
}

function statusLabel(status: string): string {
  switch (status) {
    case "not_found":
      return "Not detected";
    case "completed_with_warnings":
      return "Completed with warnings";
    default:
      return status.charAt(0).toUpperCase() + status.slice(1);
  }
}

export function MigrationsView() {
  const { user } = useSession();
  const admin = !!user?.platform_admin;
  const client = useQueryClient();

  const detectQuery = useQuery({
    ...queries.migrationDetect(),
    enabled: admin,
  });
  const jobsQuery = useQuery({
    ...queries.migrationJobs(),
    enabled: admin,
  });
  const workspacesQuery = useQuery({
    ...queries.workspaces(),
    enabled: admin,
  });
  useLiveSubscription(admin ? { topic: "system_migrations" } : null);

  const platforms = detectQuery.data?.platforms ?? [];
  const workspaces = workspacesQuery.data ?? [];
  const jobs = jobsQuery.data?.jobs ?? [];

  const [selectedPlatform, setSelectedPlatform] =
    useState<MigrationPlatform | null>(null);
  const [mode, setMode] = useState<"all" | "cherry_pick">("all");
  const [targetKind, setTargetKind] = useState<"existing" | "new">("existing");
  const [targetWorkspaceId, setTargetWorkspaceId] = useState("");
  const [createWorkspaceName, setCreateWorkspaceName] = useState("");

  const [selectedProjects, setSelectedProjects] = useState<
    Record<string, boolean>
  >({});
  const [selectedServices, setSelectedServices] = useState<
    Record<string, boolean>
  >({});
  const [projectNames, setProjectNames] = useState<Record<string, string>>({});
  const [serviceNames, setServiceNames] = useState<Record<string, string>>({});

  const [copyVolumes, setCopyVolumes] = useState(true);
  const [importDomains, setImportDomains] = useState(true);
  const [importEnv, setImportEnv] = useState(true);
  const [importLogs, setImportLogs] = useState(true);
  const [startServices, setStartServices] = useState(true);
  const [deployProjects, setDeployProjects] = useState(false);

  const activePlatform: MigrationDetectedPlatform | undefined = useMemo(() => {
    if (selectedPlatform) {
      return platforms.find((p) => p.platform === selectedPlatform);
    }
    return platforms.find((p) => p.detected) ?? platforms[0];
  }, [platforms, selectedPlatform]);

  const effectiveWorkspaceId =
    targetWorkspaceId || (workspaces.length > 0 ? workspaces[0].id : "");

  const startImport = useMutation({
    mutationFn: (body: MigrationImportRequest) =>
      request<MigrationJob>("system/migrations/import", body, "POST"),
    onSuccess: (job) => {
      client.setQueryData<{ jobs: MigrationJob[] }>(
        queries.migrationJobs().queryKey,
        (prev) => ({
          jobs: [job, ...(prev?.jobs ?? []).filter((j) => j.id !== job.id)],
        }),
      );
      void client.invalidateQueries({
        queryKey: queries.workspaces().queryKey,
      });
    },
  });

  if (!admin) {
    return (
      <section className="panel panel-pad">
        <h2>Platform Admin required</h2>
        <p className="muted">
          Only Platform Admins can scan for competitor installations and run
          migrations.
        </p>
      </section>
    );
  }

  const toggleProject = (id: string) =>
    setSelectedProjects((prev) => ({ ...prev, [id]: !prev[id] }));
  const toggleService = (id: string) =>
    setSelectedServices((prev) => ({ ...prev, [id]: !prev[id] }));

  const pickedProjects = (activePlatform?.projects ?? []).filter(
    (p) => mode === "all" || selectedProjects[p.id],
  );
  const pickedServices = (activePlatform?.services ?? []).filter(
    (s) => mode === "all" || selectedServices[s.id],
  );
  const totalSelected = pickedProjects.length + pickedServices.length;

  const canSubmit =
    !!activePlatform?.detected &&
    totalSelected > 0 &&
    !startImport.isPending &&
    (targetKind === "new"
      ? createWorkspaceName.trim().length > 0
      : effectiveWorkspaceId.length > 0);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!activePlatform || !canSubmit) return;

    const req: MigrationImportRequest = {
      platform: activePlatform.platform,
      mode,
      copy_volumes: copyVolumes,
      import_domains: importDomains,
      import_env: importEnv,
      import_logs: importLogs,
      start_services: startServices,
      deploy_projects: deployProjects,
    };

    if (targetKind === "new") {
      req.create_workspace_name = createWorkspaceName.trim();
    } else {
      req.target_workspace_id = effectiveWorkspaceId;
    }

    if (mode === "cherry_pick") {
      req.projects = pickedProjects.map((p) => ({
        source_id: p.id,
        target_name: projectNames[p.id]?.trim() || undefined,
      }));
      req.services = pickedServices.map((s) => ({
        source_id: s.id,
        target_name: serviceNames[s.id]?.trim() || undefined,
      }));
    } else {
      const projOverrides = pickedProjects
        .filter((p) => projectNames[p.id]?.trim())
        .map((p) => ({
          source_id: p.id,
          target_name: projectNames[p.id].trim(),
        }));
      const svcOverrides = pickedServices
        .filter((s) => serviceNames[s.id]?.trim())
        .map((s) => ({
          source_id: s.id,
          target_name: serviceNames[s.id].trim(),
        }));
      if (projOverrides.length > 0) req.projects = projOverrides;
      if (svcOverrides.length > 0) req.services = svcOverrides;
    }

    startImport.mutate(req);
  };

  return (
    <div className="migrations-stack">
      <section className="panel panel-pad" aria-labelledby="detect-heading">
        <div className="section-head">
          <div>
            <h2 id="detect-heading">Detected platforms</h2>
            <p className="muted small">
              Non-destructive copy: source containers, databases, and volumes on
              Openship, Dokploy, or Coolify are never modified.
            </p>
          </div>
          <button
            type="button"
            className="btn"
            disabled={detectQuery.isFetching}
            onClick={() => void detectQuery.refetch()}
          >
            <Icon name="refresh" />
            {detectQuery.isFetching ? "Scanning..." : "Rescan host"}
          </button>
        </div>

        {detectQuery.isLoading && <Loading />}
        {detectQuery.error && <ErrorMessage error={detectQuery.error} />}

        {!detectQuery.isLoading && (
          <div className="migration-platforms">
            {platforms.map((plat) => {
              const selected = activePlatform?.platform === plat.platform;
              return (
                <button
                  key={plat.platform}
                  type="button"
                  className={`migration-platform-card${selected ? " selected" : ""}${!plat.detected ? " not-found" : ""}`}
                  onClick={() => setSelectedPlatform(plat.platform)}
                >
                  <div className="migration-platform-head">
                    <span className="migration-platform-name">{plat.name}</span>
                    <span className={`badge ${statusTone(plat.status)}`}>
                      {statusLabel(plat.status)}
                    </span>
                  </div>
                  <div className="small muted">
                    {plat.detected
                      ? `${plat.projects.length} project(s) · ${plat.services.length} service(s)`
                      : plat.error || "No installation found on this node"}
                  </div>
                  {plat.data_dir && (
                    <code className="small muted">{plat.data_dir}</code>
                  )}
                </button>
              );
            })}
          </div>
        )}
      </section>

      {activePlatform && activePlatform.detected && (
        <form
          className="panel panel-pad migration-controls"
          onSubmit={handleSubmit}
          aria-labelledby="import-heading"
        >
          <div className="section-head">
            <div>
              <h2 id="import-heading">Import from {activePlatform.name}</h2>
              <p className="muted small">
                Import everything into a workspace with original names, or
                cherry-pick specific projects and services with custom names.
              </p>
            </div>
            <div
              className="migration-mode-row"
              role="group"
              aria-label="Import mode"
            >
              <button
                type="button"
                className={`btn${mode === "all" ? " primary" : ""}`}
                onClick={() => setMode("all")}
              >
                Import all
              </button>
              <button
                type="button"
                className={`btn${mode === "cherry_pick" ? " primary" : ""}`}
                onClick={() => setMode("cherry_pick")}
              >
                Cherry-pick
              </button>
            </div>
          </div>

          <div className="migration-target-row">
            <label className="field">
              <span>Target destination</span>
              <select
                value={targetKind}
                onChange={(e) =>
                  setTargetKind(e.target.value as "existing" | "new")
                }
              >
                <option value="existing">Existing workspace</option>
                <option value="new">Create new workspace</option>
              </select>
            </label>

            {targetKind === "existing" ? (
              <label className="field">
                <span>Workspace</span>
                <select
                  value={effectiveWorkspaceId}
                  onChange={(e) => setTargetWorkspaceId(e.target.value)}
                  disabled={workspaces.length === 0}
                >
                  {workspaces.length === 0 && (
                    <option value="">No workspaces available</option>
                  )}
                  {workspaces.map((ws) => (
                    <option key={ws.id} value={ws.id}>
                      {ws.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <label className="field">
                <span>New workspace name</span>
                <input
                  type="text"
                  placeholder={`Migrated from ${activePlatform.name}`}
                  value={createWorkspaceName}
                  onChange={(e) => setCreateWorkspaceName(e.target.value)}
                />
              </label>
            )}
          </div>

          <div className="migration-options-grid">
            <label className="migration-option">
              <input
                type="checkbox"
                checked={copyVolumes}
                onChange={(e) => setCopyVolumes(e.target.checked)}
              />
              <span>Copy volumes (non-destructive)</span>
            </label>
            <label className="migration-option">
              <input
                type="checkbox"
                checked={importEnv}
                onChange={(e) => setImportEnv(e.target.checked)}
              />
              <span>Import environment variables</span>
            </label>
            <label className="migration-option">
              <input
                type="checkbox"
                checked={importDomains}
                onChange={(e) => setImportDomains(e.target.checked)}
              />
              <span>Import domains &amp; routes</span>
            </label>
            <label className="migration-option">
              <input
                type="checkbox"
                checked={importLogs}
                onChange={(e) => setImportLogs(e.target.checked)}
              />
              <span>Import historical logs</span>
            </label>
            <label className="migration-option">
              <input
                type="checkbox"
                checked={startServices}
                onChange={(e) => setStartServices(e.target.checked)}
              />
              <span>Start migrated services</span>
            </label>
            <label className="migration-option">
              <input
                type="checkbox"
                checked={deployProjects}
                onChange={(e) => setDeployProjects(e.target.checked)}
              />
              <span>Trigger initial project deploys</span>
            </label>
          </div>

          {activePlatform.projects.length > 0 && (
            <div>
              <h3>Projects ({activePlatform.projects.length})</h3>
              <div className="migration-items-list">
                {activePlatform.projects.map((proj) => {
                  const checked = mode === "all" || !!selectedProjects[proj.id];
                  return (
                    <div
                      key={proj.id}
                      className={`migration-item-row${mode === "all" ? " no-check" : ""}`}
                    >
                      {mode === "cherry_pick" && (
                        <input
                          type="checkbox"
                          aria-label={`Select project ${proj.name}`}
                          checked={checked}
                          onChange={() => toggleProject(proj.id)}
                        />
                      )}
                      <div>
                        <div className="migration-item-title">
                          <span>{proj.name}</span>
                          {proj.source_project && (
                            <span className="badge muted">
                              {proj.source_project}
                            </span>
                          )}
                          {proj.environment && (
                            <span className="badge muted">
                              {proj.environment}
                            </span>
                          )}
                          {proj.build_type && (
                            <span className="badge info">
                              {proj.build_type}
                            </span>
                          )}
                        </div>
                        <div className="migration-item-meta">
                          {proj.git_url && <span>{proj.git_url}</span>}
                          {!proj.git_url && proj.docker_image && (
                            <span>Image: {proj.docker_image}</span>
                          )}
                          <span>{proj.env_count} env var(s)</span>
                          <span>{proj.volumes?.length ?? 0} volume(s)</span>
                          <span>{proj.domains?.length ?? 0} domain(s)</span>
                          <span>{proj.log_count} log(s)</span>
                        </div>
                        {proj.warnings && proj.warnings.length > 0 && (
                          <div className="small muted">
                            {proj.warnings.join(" · ")}
                          </div>
                        )}
                      </div>
                      <input
                        type="text"
                        aria-label={`Target name for ${proj.name}`}
                        placeholder={proj.name}
                        value={projectNames[proj.id] ?? ""}
                        disabled={!checked}
                        onChange={(e) =>
                          setProjectNames((prev) => ({
                            ...prev,
                            [proj.id]: e.target.value,
                          }))
                        }
                      />
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {activePlatform.services.length > 0 && (
            <div>
              <h3>Services ({activePlatform.services.length})</h3>
              <div className="migration-items-list">
                {activePlatform.services.map((svc) => {
                  const checked = mode === "all" || !!selectedServices[svc.id];
                  return (
                    <div
                      key={svc.id}
                      className={`migration-item-row${mode === "all" ? " no-check" : ""}`}
                    >
                      {mode === "cherry_pick" && (
                        <input
                          type="checkbox"
                          aria-label={`Select service ${svc.name}`}
                          checked={checked}
                          onChange={() => toggleService(svc.id)}
                        />
                      )}
                      <div>
                        <div className="migration-item-title">
                          <span>{svc.name}</span>
                          <span className="badge info">
                            {svc.engine || svc.raw_engine || "service"}
                            {svc.version ? ` ${svc.version}` : ""}
                          </span>
                          {!svc.compatible && (
                            <span className="badge warn">
                              Unsupported engine
                            </span>
                          )}
                        </div>
                        <div className="migration-item-meta">
                          {svc.image && <span>{svc.image}</span>}
                          <span>{svc.volumes?.length ?? 0} volume(s)</span>
                          {svc.external_port ? (
                            <span>Port {svc.external_port}</span>
                          ) : null}
                        </div>
                      </div>
                      <input
                        type="text"
                        aria-label={`Target name for ${svc.name}`}
                        placeholder={svc.name}
                        value={serviceNames[svc.id] ?? ""}
                        disabled={!checked}
                        onChange={(e) =>
                          setServiceNames((prev) => ({
                            ...prev,
                            [svc.id]: e.target.value,
                          }))
                        }
                      />
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {startImport.error && <ErrorMessage error={startImport.error} />}

          <div className="section-head">
            <span className="small muted">
              {totalSelected} item(s) ready to copy into{" "}
              {targetKind === "new"
                ? createWorkspaceName.trim() || "new workspace"
                : workspaces.find((w) => w.id === effectiveWorkspaceId)?.name ||
                  "selected workspace"}
            </span>
            <button type="submit" className="btn primary" disabled={!canSubmit}>
              <Icon name="download" />
              {startImport.isPending
                ? "Starting migration..."
                : mode === "all"
                  ? `Import all (${totalSelected})`
                  : `Import selected (${totalSelected})`}
            </button>
          </div>
        </form>
      )}

      <section className="panel panel-pad" aria-labelledby="jobs-heading">
        <div className="section-head">
          <div>
            <h2 id="jobs-heading">Migration history</h2>
            <p className="muted small">
              Live progress and results from recent competitor imports.
            </p>
          </div>
        </div>

        {jobsQuery.isLoading && (
          <Loading layout="table" label="Loading migration jobs…" />
        )}
        {jobsQuery.error && <ErrorMessage error={jobsQuery.error} />}

        {!jobsQuery.isLoading && jobs.length === 0 && (
          <p className="muted small">No migrations have been run yet.</p>
        )}

        {jobs.length > 0 && (
          <div className="migration-items-list">
            {jobs.map((job) => (
              <div key={job.id} className="migration-job-card">
                <div className="migration-job-head">
                  <div className="migration-item-title">
                    <span>
                      {job.platform} ({job.mode})
                    </span>
                    <span className={`badge ${statusTone(job.status)}`}>
                      {statusLabel(job.status)}
                    </span>
                    {job.target_workspace_name && (
                      <span className="small muted">
                        → {job.target_workspace_name}
                      </span>
                    )}
                  </div>
                  <span className="small muted">
                    {job.projects.length} project(s) · {job.services.length}{" "}
                    service(s)
                  </span>
                </div>
                {job.error && (
                  <div className="small" style={{ color: "var(--danger)" }}>
                    {job.error}
                  </div>
                )}
                {job.logs && job.logs.length > 0 && (
                  <div className="migration-job-logs">
                    {job.logs.join("\n")}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
