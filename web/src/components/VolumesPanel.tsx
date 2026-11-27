import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import type { Project, Volume } from "../services/types";
import { queries } from "../services/queries";
import {
  useServices,
  useResourceDeployNeeded,
  useVolumes,
  validServiceName,
} from "../services/services";
import { request, remove } from "../services/api";
import { useSession } from "../providers/SessionProvider";
import { useDashboard } from "../layouts/DashboardLayout";
import { useUI } from "../providers/UIProvider";
import { ServiceModal, ServicePicker, RedeployNote } from "./ServiceUI";
import { AsyncForm, Empty, ErrorMessage, Field, Loading } from "./ui";
import { Icon } from "./Icon";
import { Heading } from "./Heading";
import { ProjectHeading } from "./ProjectHeading";
import { ResourceTransfer } from "./ResourceTransfer";

export function volumeSize(bytes: number) {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"],
    index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), 4);
  return `${(bytes / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}
function VolumeModal({
  workspaceId,
  volumes,
  project,
  onClose,
  onSaved,
}: {
  workspaceId: string;
  volumes: Volume[];
  project?: Project;
  onClose: () => void;
  onSaved: () => Promise<unknown>;
}) {
  const [mode, setMode] = useState("new"),
    [name, setName] = useState(""),
    [mount, setMount] = useState("/data"),
    [selected, setSelected] = useState("");
  const available = volumes.filter(
      (v) => !v.project_id && !v.service_id && !v.purpose,
    ),
    mountValid =
      mount.startsWith("/") &&
      mount !== "/" &&
      !mount.split("/").includes("..");
  const valid =
    (mode === "new"
      ? validServiceName(name)
      : available.some((v) => v.id === selected)) &&
    (!project || mountValid);
  return (
    <ServiceModal
      title={project ? "Add a volume" : "Create a volume"}
      description="Persistent storage that stays with your data, not your container."
      onClose={onClose}
    >
      <AsyncForm
        onSubmit={async () => {
          if (mode === "existing")
            await request(
              `volumes/${selected}`,
              { project_id: project!.id, mount_path: mount },
              "PATCH",
            );
          else
            await request(`workspaces/${workspaceId}/volumes`, {
              name,
              ...(project ? { project_id: project.id, mount_path: mount } : {}),
            });
          await onSaved();
          onClose();
        }}
      >
        <div className="service-config-body">
          {project && (
            <div
              className="service-tabs"
              role="group"
              aria-label="Volume source"
            >
              <button
                type="button"
                className={mode === "new" ? "active" : ""}
                onClick={() => setMode("new")}
              >
                Create new
              </button>
              <button
                type="button"
                className={mode === "existing" ? "active" : ""}
                onClick={() => setMode("existing")}
              >
                Attach existing
              </button>
            </div>
          )}
          {mode === "new" ? (
            <>
              <Field
                label="Volume name"
                name="name"
                value={name}
                required
                maxLength={63}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. uploads"
              />
              {name && !validServiceName(name) && (
                <span className="service-validation">
                  Start with a lowercase letter. Use a–z, 0–9, _ or -.
                </span>
              )}
            </>
          ) : (
            <>
              <ServicePicker
                label="Workspace volume"
                value={selected}
                options={available.map((v) => ({
                  value: v.id,
                  label: `${v.name} · ${volumeSize(v.size_bytes)}`,
                }))}
                onChange={setSelected}
              />
              {!available.length && (
                <p>
                  No unattached workspace volumes are available. Create a new
                  volume instead.
                </p>
              )}
            </>
          )}
          {project && (
            <>
              <Field
                label="Mount path"
                name="mount_path"
                value={mount}
                required
                onChange={(e) => setMount(e.target.value)}
                placeholder="/data"
              />
              {!mountValid && (
                <span className="service-validation">
                  Use an absolute container path other than /, without parent
                  traversal.
                </span>
              )}
            </>
          )}
          <div className="service-persistence">
            <Icon name="hard-drive" />
            <span>
              <strong>Built to outlast a deploy</strong>
              <span>
                Files are kept across container recreations. Detaching a volume
                does not delete its data.
              </span>
            </span>
          </div>
        </div>
        <div className="service-modal-foot">
          <button type="button" onClick={onClose}>
            Cancel
          </button>
          <button className="primary" disabled={!valid}>
            <Icon name="hard-drive" />
            {mode === "existing" ? "Attach volume" : "Create volume"}
          </button>
        </div>
      </AsyncForm>
    </ServiceModal>
  );
}
export function VolumesPanel({
  workspaceId,
  project,
}: {
  workspaceId: string;
  project?: Project;
}) {
  const { workspace } = useDashboard(),
    { can } = useSession(),
    ui = useUI(),
    client = useQueryClient(),
    volumes = useVolumes(workspaceId),
    services = useServices(workspaceId),
    projects = useQuery(queries.projects(workspaceId)),
    catalog = useQuery(queries.catalog());
  const [open, setOpen] = useState(false),
    changed = useResourceDeployNeeded(project),
    allowed =
      can("services.manage", workspace) &&
      (!project || can("project.settings", project));
  const rows = (volumes.data?.volumes || []).filter(
    (v) => !project || v.project_id === project.id,
  );
  async function refresh() {
    await client.invalidateQueries();
  }
  const actions = allowed && (
    <button className="primary" onClick={() => setOpen(true)}>
      <Icon name="plus" />
      {project ? "Add volume" : "Create volume"}
    </button>
  );
  return (
    <div className="services-page">
      {project ? (
        <ProjectHeading title="Volumes" actions={actions} />
      ) : (
        <Heading
          title="Volumes"
          kicker={workspace?.name}
          subtitle="Persistent storage for your projects and services. Your data stays yours."
          actions={actions}
        />
      )}
      {project && (
        <p className="resource-intro">
          Durable storage mounted inside your application container.
        </p>
      )}
      {changed && <RedeployNote />}
      <ErrorMessage error={volumes.error || volumes.liveError} />
      {volumes.isPending ? (
        <Loading />
      ) : !rows.length ? (
        <Empty
          title={
            project ? "Give your files a permanent home" : "Storage that stays"
          }
        >
          <p>
            {project
              ? "Mount a new or existing volume to keep files across deployments."
              : "Service data volumes appear here automatically. Create a volume for project files whenever you need one."}
          </p>
          {allowed && (
            <button onClick={() => setOpen(true)}>
              {project ? "Add volume" : "Create volume"}
            </button>
          )}
        </Empty>
      ) : (
        <>
          <div className="service-list-meta">
            <span>
              {rows.length} {project ? "mounted " : ""}volume
              {rows.length !== 1 ? "s" : ""}
            </span>
            <span className="small muted">Size refreshes on the server</span>
          </div>
          <div className={`volumes-table ${project ? "project-volumes" : ""}`}>
            <div className="volume-table-head">
              <span>Name</span>
              <span>Size</span>
              {!project && <span>Used by</span>}
              <span>Mount path</span>
              <span>Created</span>
              <span />
            </div>
            {rows.map((v) => {
              const inUse = !!(v.service_id || v.project_id),
                service = services.data?.services.find(
                  (s) => s.id === v.service_id,
                ),
                mountPath =
                  v.mount_path ||
                  service?.definition?.data_path ||
                  catalog.data?.find((e) => e.id === service?.engine)
                    ?.data_path,
                owner = v.service_id
                  ? services.data?.services.find((s) => s.id === v.service_id)
                      ?.name
                  : projects.data?.find((p) => p.id === v.project_id)?.name;
              return (
                <div className="volume-table-row" key={v.id}>
                  <div className="volume-name">
                    <span className="volume-mark">
                      <Icon name="hard-drive" />
                    </span>
                    <strong>{v.name}</strong>
                  </div>
                  <span
                    className="volume-size"
                    title={
                      v.size_updated
                        ? `Updated ${new Date(v.size_updated).toLocaleString()}`
                        : "Size not measured yet"
                    }
                  >
                    {v.size_updated ? volumeSize(v.size_bytes) : "—"}
                  </span>
                  {!project && (
                    <span className="volume-owner">
                      {v.purpose === "backups" ? (
                        <Link
                          className="attachment-chip"
                          to={`/workspace/${workspaceId}/backups`}
                        >
                          <Icon name="history" />
                          Service backups
                        </Link>
                      ) : inUse ? (
                        <Link
                          className="attachment-chip"
                          to={
                            v.service_id
                              ? `/workspace/${workspaceId}/services/${v.service_id}`
                              : `/project/${v.project_id}/volumes`
                          }
                        >
                          <Icon name={v.service_id ? "database" : "box"} />
                          {owner || (v.service_id ? "Service" : "Project")}
                        </Link>
                      ) : (
                        <span className="muted small">Unattached</span>
                      )}
                    </span>
                  )}
                  <code className="volume-path">
                    {(v.purpose === "backups" ? "/backups" : mountPath) || "—"}
                  </code>
                  <time className="volume-created" dateTime={v.created}>
                    {new Date(v.created).toLocaleDateString(undefined, {
                      month: "short",
                      day: "numeric",
                      year: "numeric",
                    })}
                  </time>
                  <div className="volume-actions">
                    {allowed && v.purpose !== "backups" && (
                      <ResourceTransfer
                        kind="volumes"
                        id={v.id}
                        name={v.name}
                        workspaceId={workspaceId}
                      />
                    )}
                    {allowed &&
                      (project ? (
                        <button
                          aria-label={`Detach ${v.name}`}
                          onClick={() =>
                            ui.dialog({
                              title: `Detach ${v.name}?`,
                              description:
                                "Data is kept. Redeploy the project to stop mounting this volume.",
                              submit: "Detach volume",
                              onSubmit: async () => {
                                await request(
                                  `volumes/${v.id}`,
                                  { project_id: "", mount_path: "" },
                                  "PATCH",
                                );
                                await refresh();
                              },
                            })
                          }
                        >
                          Detach
                        </button>
                      ) : (
                        <span
                          title={
                            inUse
                              ? "Detach this volume from its project or service before deleting."
                              : "Delete volume"
                          }
                        >
                          <button
                            className="icon-button danger"
                            disabled={inUse}
                            aria-label={`Delete ${v.name}`}
                            onClick={() =>
                              ui.dialog({
                                title: `Delete ${v.name}?`,
                                description:
                                  "This permanently deletes the volume and all of its data. This cannot be undone.",
                                submit: "Delete volume",
                                onSubmit: async () => {
                                  await remove(`volumes/${v.id}`);
                                  await refresh();
                                },
                              })
                            }
                          >
                            <Icon name="trash" />
                          </button>
                        </span>
                      ))}
                  </div>
                </div>
              );
            })}
          </div>
          <div className="service-bottom-note">
            <Icon name="hard-drive" />
            Volumes survive redeploys.{" "}
            {project
              ? "Mount changes apply on your next deployment."
              : "In-use volumes must be detached before they can be deleted."}
          </div>
        </>
      )}
      {open && (
        <VolumeModal
          workspaceId={workspaceId}
          volumes={volumes.data?.volumes || []}
          project={project}
          onClose={() => setOpen(false)}
          onSaved={refresh}
        />
      )}
    </div>
  );
}
