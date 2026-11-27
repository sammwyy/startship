import { useState } from "react";
import { Link } from "react-router-dom";
import { useUI } from "../providers/UIProvider";
import { request, remove } from "../services/api";
import { useBackups } from "../services/services";
import type { Backup, BackupPolicy, Service } from "../services/types";
import { Badge, Empty, ErrorMessage, Loading } from "./ui";
import { Icon } from "./Icon";
import { EngineIcon } from "./EngineIcon";
import { ServicePicker } from "./ServiceUI";
import { volumeSize } from "./VolumesPanel";
import "./BackupsPanel.css";

const pad = (n: number) => String(n).padStart(2, "0");
const days = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];
export const defaultPolicy: BackupPolicy = {
  enabled: false,
  frequency: "daily",
  hour: 3,
  minute: 0,
  weekday: 0,
  keep: 7,
};
export function scheduleLabel(p?: BackupPolicy) {
  if (!p?.enabled) return "Not scheduled";
  const time = `${pad(p.hour)}:${pad(p.minute)} UTC`;
  if (p.frequency === "hourly") return `Every hour at :${pad(p.minute)}`;
  if (p.frequency === "weekly") return `${days[p.weekday]}s at ${time}`;
  return `Daily at ${time}`;
}
const when = (value: string) =>
  new Date(value).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
const triggers: Record<Backup["trigger"], string> = {
  scheduled: "Scheduled",
  manual: "Manual",
  "pre-restore": "Before restore",
};
export function backupTargets(backup: Backup, services: Service[]) {
  return services.filter((s) => s.engine === backup.engine && s.volume_id);
}

function RestoreTarget({
  backup,
  services,
}: {
  backup: Backup;
  services: Service[];
}) {
  const targets = backupTargets(backup, services),
    [value, setValue] = useState(
      targets.some((s) => s.id === backup.service_id)
        ? backup.service_id
        : targets[0]?.id || "",
    );
  return (
    <>
      <ServicePicker
        label="Restore into"
        value={value}
        options={targets.map((s) => ({
          value: s.id,
          label: `${s.name} · ${s.version}${s.id === backup.service_id ? " (original)" : ""}`,
        }))}
        onChange={setValue}
      />
      <input type="hidden" name="service_id" value={value} />
    </>
  );
}

export function ScheduleEditor({
  service,
  disabled,
}: {
  service: Service;
  disabled?: boolean;
}) {
  const ui = useUI(),
    initial = service.backup || defaultPolicy,
    [p, setP] = useState<BackupPolicy>(initial),
    [keep, setKeep] = useState(String(initial.keep)),
    [saving, setSaving] = useState(false);
  const keepValue = Number(keep),
    keepValid =
      Number.isInteger(keepValue) && keepValue >= 1 && keepValue <= 100,
    next = { ...p, keep: keepValue },
    dirty = JSON.stringify(next) !== JSON.stringify(initial),
    set = (patch: Partial<BackupPolicy>) => setP({ ...p, ...patch });
  const local = new Date(
    Date.UTC(2000, 0, 2 + p.weekday, p.hour, p.minute),
  ).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return (
    <section className="panel service-panel backup-schedule">
      <div className="service-section-head">
        <h2>Schedule</h2>
        <button
          type="button"
          className="service-switch"
          role="switch"
          aria-label="Scheduled backups"
          aria-checked={p.enabled}
          disabled={disabled}
          onClick={() => set({ enabled: !p.enabled })}
        >
          <span />
        </button>
      </div>
      <p>
        {p.enabled
          ? "Copies are taken automatically and the oldest scheduled copies are rotated out."
          : "Turn on to copy this service’s volume on a recurring schedule."}
      </p>
      <fieldset className="backup-fields" disabled={!p.enabled || disabled}>
        <div className="backup-frequency" role="group" aria-label="Frequency">
          {(["hourly", "daily", "weekly"] as const).map((f) => (
            <button
              type="button"
              key={f}
              aria-pressed={p.frequency === f}
              onClick={() => set({ frequency: f })}
            >
              {f[0].toUpperCase() + f.slice(1)}
            </button>
          ))}
        </div>
        <div className="backup-time">
          {p.frequency === "weekly" && (
            <ServicePicker
              label="Day"
              value={String(p.weekday)}
              disabled={!p.enabled || disabled}
              options={days.map((d, i) => ({ value: String(i), label: d }))}
              onChange={(v) => set({ weekday: Number(v) })}
            />
          )}
          {p.frequency !== "hourly" && (
            <ServicePicker
              label="Hour (UTC)"
              value={String(p.hour)}
              disabled={!p.enabled || disabled}
              options={Array.from({ length: 24 }, (_, i) => ({
                value: String(i),
                label: pad(i),
              }))}
              onChange={(v) => set({ hour: Number(v) })}
            />
          )}
          <ServicePicker
            label="Minute"
            value={String(p.minute)}
            disabled={!p.enabled || disabled}
            options={Array.from(
              new Set([
                ...Array.from({ length: 12 }, (_, i) => i * 5),
                p.minute,
              ]),
            )
              .sort((a, b) => a - b)
              .map((m) => ({ value: String(m), label: `:${pad(m)}` }))}
            onChange={(v) => set({ minute: Number(v) })}
          />
          <label className="backup-keep">
            <span className="service-field-label">Keep</span>
            <input
              type="number"
              min={1}
              max={100}
              value={keep}
              aria-label="Scheduled copies to keep"
              aria-invalid={!keepValid}
              onChange={(e) => setKeep(e.target.value)}
            />
          </label>
        </div>
        <span className="small muted backup-local">
          {p.frequency === "hourly"
            ? `Runs at minute ${pad(p.minute)} of every hour.`
            : `${p.frequency === "weekly" ? days[p.weekday] + " " : ""}${pad(p.hour)}:${pad(p.minute)} UTC is ${local} your time.`}{" "}
          Keeps the latest {keepValid ? keepValue : "—"} scheduled copies.
        </span>
      </fieldset>
      <div className="backup-schedule-foot">
        <span className="small muted">
          {service.backup?.enabled && service.next_backup
            ? `Next: ${when(service.next_backup)}`
            : "No backup scheduled"}
        </span>
        <button
          className="primary"
          disabled={!dirty || !keepValid || saving || disabled}
          onClick={() =>
            ui.run(async () => {
              setSaving(true);
              try {
                await request(
                  `services/${service.id}/backup-schedule`,
                  next,
                  "PUT",
                );
                ui.notify(
                  next.enabled ? "Backup schedule saved" : "Backups paused",
                );
              } finally {
                setSaving(false);
              }
            })
          }
        >
          Save schedule
        </button>
      </div>
    </section>
  );
}

export function BackupsPanel({
  workspaceId,
  services,
  service,
  allowed,
}: {
  workspaceId: string;
  services: Service[];
  service?: Service;
  allowed: boolean;
}) {
  const ui = useUI(),
    data = useBackups(workspaceId);
  const rows = (data.data?.backups || []).filter(
      (b) => !service || b.service_id === service.id,
    ),
    busy =
      service?.status === "restoring" ||
      rows.some((b) => b.status === "running");
  function restore(b: Backup) {
    const original = services.find((s) => s.id === b.service_id);
    ui.dialog({
      title: "Restore this backup?",
      description: `The target service stops while its data is replaced with the copy from ${when(b.created)}. A safety backup of its current data is taken first.`,
      fields:
        service || !backupTargets(b, services).length ? undefined : (
          <RestoreTarget backup={b} services={services} />
        ),
      submit: "Restore",
      onSubmit: async (form) => {
        const target = String(form.get("service_id") || original?.id || "");
        if (!target) throw new Error(`No ${b.engine} service to restore into`);
        await request(`backups/${b.id}/restore`, { service_id: target });
        ui.notify("Restore started");
      },
    });
  }
  return (
    <section className="panel backup-history">
      <div className="service-section-head backup-history-head">
        <h2>
          {service ? "Backups" : "History"}
          <span className="count">{rows.length}</span>
        </h2>
        {service && allowed && (
          <button
            disabled={busy || !service.volume_id}
            onClick={() =>
              ui.run(async () => {
                await request(`services/${service.id}/backups`, {});
                ui.notify("Backup started");
              })
            }
          >
            <Icon name="history" />
            Back up now
          </button>
        )}
      </div>
      <ErrorMessage error={data.error || data.liveError} />
      {data.isPending ? (
        <Loading layout="table" label="Loading backups…" />
      ) : !rows.length ? (
        <Empty title="No backups yet">
          <p>
            {service
              ? "Take a copy now or turn on a schedule."
              : "Schedule backups from a service’s Backups tab."}
          </p>
        </Empty>
      ) : (
        <ul className="backup-list">
          {rows.map((b) => (
            <li key={b.id} className="backup-row">
              {service ? (
                <span className="volume-mark">
                  <Icon name="archive" />
                </span>
              ) : (
                <EngineIcon engine={b.engine} />
              )}
              <div className="backup-main">
                <strong>
                  {service ? (
                    when(b.created)
                  ) : (
                    <Link
                      to={`/workspace/${workspaceId}/services/${b.service_id}`}
                    >
                      {b.service_name}
                    </Link>
                  )}
                </strong>
                <span>
                  {!service && `${when(b.created)} · `}
                  {triggers[b.trigger]} · {b.engine} {b.version}
                  {b.status === "completed" && ` · ${volumeSize(b.size_bytes)}`}
                </span>
                {b.error && <span className="danger">{b.error}</span>}
              </div>
              <Badge status={b.status} />
              <div className="backup-actions">
                {allowed && b.status === "completed" && (
                  <a
                    className="button icon-button"
                    href={`/api/backups/${b.id}/download`}
                    download
                    aria-label={`Download backup from ${when(b.created)}`}
                    title="Download"
                  >
                    <Icon name="download" />
                  </a>
                )}
                {allowed && b.status === "completed" && (
                  <button
                    className="icon-button"
                    disabled={busy}
                    aria-label={`Restore backup from ${when(b.created)}`}
                    title="Restore"
                    onClick={() => restore(b)}
                  >
                    <Icon name="restore" />
                  </button>
                )}
                {allowed && b.status !== "running" && (
                  <button
                    className="icon-button"
                    aria-label={`Delete backup from ${when(b.created)}`}
                    title="Delete"
                    onClick={() =>
                      ui.dialog({
                        title: "Delete this backup?",
                        description:
                          "The copy is removed from the workspace backups volume. This cannot be undone.",
                        submit: "Delete backup",
                        onSubmit: () => remove(`backups/${b.id}`),
                      })
                    }
                  >
                    <Icon name="trash" />
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export function BackupStorage({ workspaceId }: { workspaceId: string }) {
  const data = useBackups(workspaceId),
    v = data.data?.volume;
  return (
    <section className="panel service-panel">
      <div className="service-section-head">
        <h2>Storage</h2>
        <Icon name="hard-drive" />
      </div>
      {v ? (
        <p>
          Copies live in the workspace volume{" "}
          <Link to={`/workspace/${workspaceId}/volumes`}>
            <code>{v.name}</code>
          </Link>
          {v.size_updated ? `, ${volumeSize(v.size_bytes)} used` : ""}.
        </p>
      ) : (
        <p>
          The first backup creates a <code>service-backups</code> volume in this
          workspace to hold every copy.
        </p>
      )}
    </section>
  );
}
