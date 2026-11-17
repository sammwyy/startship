import type { CSSProperties } from "react";
import type { AuditEntry, Permission, Role } from "../services/types";
import { Icon } from "./Icon";
import { Empty } from "./ui";

export const roleStyle = (color?: string) =>
  ({ "--role": color || "#8b8d98" }) as CSSProperties;
export function RoleIcon({ role }: { role?: Pick<Role, "color" | "icon"> }) {
  return (
    <span className="role-icon" style={roleStyle(role?.color)}>
      <Icon name={role?.icon || "user"} />
    </span>
  );
}
export function RoleChip({
  role,
  suffix,
  onRemove,
}: {
  role?: Role;
  suffix?: string;
  onRemove?: () => void;
}) {
  return (
    <span className="role-chip" style={roleStyle(role?.color)}>
      <Icon name={role?.icon || "user"} />
      <span>{role?.name || "Deleted role"}</span>
      {suffix && <small>{suffix}</small>}
      {onRemove && (
        <button
          type="button"
          className="chip-remove"
          aria-label={`Remove ${role?.name || "role"}`}
          onClick={onRemove}
        >
          <Icon name="close" />
        </button>
      )}
    </span>
  );
}
export function groupPermissions(permissions: Permission[]) {
  return [...new Set(permissions.map((p) => p.group))].map(
    (group) => [group, permissions.filter((p) => p.group === group)] as const,
  );
}
export function AuditTable({ entries }: { entries: AuditEntry[] }) {
  return entries.length ? (
    <div className="panel table-panel">
      <table className="audit-table">
        <thead>
          <tr>
            <th>When</th>
            <th>Who</th>
            <th>Change</th>
            <th>Target</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((entry, i) => (
            <tr key={entry.id || i}>
              <td>{new Date(entry.at).toLocaleString()}</td>
              <td>{entry.actor}</td>
              <td>
                <span className={`badge audit-${entry.action.split(".")[1]}`}>
                  {entry.action.replace(".", " ")}
                </span>
              </td>
              <td>
                {entry.target}
                {entry.scope !== "platform" && <small> {entry.scope}</small>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  ) : (
    <Empty title="No changes yet">
      Changes to roles, members and overrides show up here.
    </Empty>
  );
}
