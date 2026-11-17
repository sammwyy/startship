import { Suspense, useState } from "react";
import { useParams } from "react-router-dom";
import {
  useQuery,
  useQueryClient,
  useSuspenseQueries,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { queries } from "../../services/queries";
import { remove, request } from "../../services/api";
import type {
  Member,
  Override,
  Permission,
  Project,
  Role,
} from "../../services/types";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { text } from "../../lib/project";
import { Heading } from "../../components/Heading";
import { Icon } from "../../components/Icon";
import { AsyncForm, Loading } from "../../components/ui";
import {
  AuditTable,
  groupPermissions,
  RoleChip,
} from "../../components/Access";

function OverrideEditor({
  workspaceId,
  projects,
  roles,
  members,
  permissions,
}: {
  workspaceId: string;
  projects: Project[];
  roles: Role[];
  members: Member[];
  permissions: Permission[];
}) {
  const [target, setTarget] = useState(`workspace:${workspaceId}`),
    [subject, setSubject] = useState(
      `role:${roles.find((r) => !r.fixed)?.id || ""}`,
    );
  const [scope, scopeId] = target.split(":"),
    [subjectType, subjectId] = subject.split(":");
  const path = `overrides?scope=${scope}&scope_id=${encodeURIComponent(scopeId)}`,
    ui = useUI(),
    client = useQueryClient();
  const {
    data = [],
    isPending,
    error,
  } = useQuery({
    queryKey: [path],
    queryFn: ({ signal }) =>
      request<Override[]>(path, undefined, "GET", signal),
  });
  const current = data.find(
    (o) => o.subject_type === subjectType && o.subject_id === subjectId,
  );
  const available = permissions.filter(
    (p) =>
      p.scope === "project" ||
      (scope === "workspace" && p.scope === "workspace"),
  );
  return (
    <section className="override-section">
      <div className="section-head">
        <h2>Permission overrides</h2>
      </div>
      <div className="panel override-editor">
        <div className="override-target">
          <label>
            Where
            <select value={target} onChange={(e) => setTarget(e.target.value)}>
              <option value={`workspace:${workspaceId}`}>
                Whole workspace
              </option>
              {projects.map((p) => (
                <option key={p.id} value={`project:${p.id}`}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Who
            <select
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
            >
              <optgroup label="Roles">
                {roles
                  .filter((r) => !r.fixed)
                  .map((r) => (
                    <option key={r.id} value={`role:${r.id}`}>
                      {r.name}
                    </option>
                  ))}
              </optgroup>
              <optgroup label="Members">
                {members.map((m) => (
                  <option key={m.user_id} value={`user:${m.user_id}`}>
                    {m.username}
                  </option>
                ))}
              </optgroup>
            </select>
          </label>
        </div>
        {isPending ? (
          <Loading />
        ) : (
          <AsyncForm
            key={`${target}-${subject}-${JSON.stringify(current)}`}
            disabled={!!error}
            onSubmit={async (form) => {
              const picked = (state: string) =>
                available
                  .filter((p) => form.get(p.key) === state)
                  .map((p) => p.key);
              await request(
                "overrides",
                {
                  scope,
                  scope_id: scopeId,
                  subject_type: subjectType,
                  subject_id: subjectId,
                  allow: picked("allow"),
                  deny: picked("deny"),
                },
                "PUT",
              );
              await client.invalidateQueries({ queryKey: [path] });
              ui.notify("Overrides saved");
            }}
          >
            {groupPermissions(available).map(([group, items]) => (
              <fieldset className="permission-group" key={group}>
                <legend>{group}</legend>
                {items.map((p) => (
                  <div className="permission-row" key={p.key}>
                    <span>
                      <strong>{p.label}</strong>
                      <small>{p.description}</small>
                    </span>
                    <div
                      className="tri-state"
                      role="radiogroup"
                      aria-label={p.label}
                    >
                      {[
                        ["deny", "Deny", "close"],
                        ["inherit", "Inherit", "settings"],
                        ["allow", "Allow", "plus"],
                      ].map(([value, label, icon]) => (
                        <label
                          key={value}
                          className={`tri-${value}`}
                          title={label}
                        >
                          <input
                            type="radio"
                            name={p.key}
                            value={value}
                            defaultChecked={
                              (current?.deny.includes(p.key)
                                ? "deny"
                                : current?.allow.includes(p.key)
                                  ? "allow"
                                  : "inherit") === value
                            }
                          />
                          <Icon name={icon} />
                          <span>{label}</span>
                        </label>
                      ))}
                    </div>
                  </div>
                ))}
              </fieldset>
            ))}
            {error && <p role="alert">{error.message}</p>}
            <div className="role-actions">
              <span className="small">
                Deny removes a permission granted by roles; allow adds one.
                Project rules beat workspace rules and member rules beat role
                rules.
              </span>
              <button className="primary">Save overrides</button>
            </div>
          </AsyncForm>
        )}
      </div>
    </section>
  );
}
function RecentAudit({ workspaceId }: { workspaceId: string }) {
  const { data } = useSuspenseQuery(queries.audit(workspaceId));
  return (
    <section>
      <div className="section-head">
        <h2>Recent changes</h2>
      </div>
      <AuditTable entries={data.slice(0, 20)} />
    </section>
  );
}
export function MembersView() {
  const id = useParams().workspaceId!,
    { workspace } = useDashboard(),
    { user } = useSession(),
    ui = useUI(),
    client = useQueryClient();
  const path = `workspaces/${id}/members`;
  const [
    { data },
    { data: roles },
    { data: catalog },
    { data: projects },
    { data: users },
  ] = useSuspenseQueries({
    queries: [
      {
        queryKey: [path],
        queryFn: () =>
          request<{ members: Member[]; can_manage: boolean }>(path),
      },
      queries.roles(),
      queries.permissions(),
      queries.projects(id),
      queries.users(),
    ],
  });
  const refresh = () => client.invalidateQueries();
  const add = () =>
    ui.dialog({
      title: "Add member",
      description:
        "Give someone a role in this workspace or in one of its projects.",
      submit: "Add member",
      fields: (
        <>
          <label>
            User
            <select name="user_id" required>
              {users.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.username}
                </option>
              ))}
            </select>
          </label>
          <label>
            Role
            <select name="role_id" defaultValue="member">
              {roles
                .filter((r) => !r.fixed)
                .map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.name}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Scope
            <select name="scope">
              <option value={`workspace:${id}`}>Whole workspace</option>
              {projects.map((p) => (
                <option key={p.id} value={`project:${p.id}`}>
                  Project · {p.name}
                </option>
              ))}
            </select>
          </label>
        </>
      ),
      onSubmit: async (form) => {
        const [scope, scopeId] = text(form, "scope").split(":");
        await request("assignments", {
          user_id: text(form, "user_id"),
          role_id: text(form, "role_id"),
          scope,
          scope_id: scopeId,
        });
        await refresh();
        ui.notify("Member added");
      },
    });
  return (
    <>
      <Heading
        kicker="Workspace"
        title="Members"
        subtitle={`Who can reach ${workspace?.name || "this workspace"} and what their roles let them do.`}
        actions={
          data.can_manage && (
            <button className="primary" onClick={add}>
              <Icon name="plus" />
              Add member
            </button>
          )
        }
      />
      <div className="panel table-panel">
        <table>
          <thead>
            <tr>
              <th>Member</th>
              <th>Roles</th>
            </tr>
          </thead>
          <tbody>
            {data.members.map((m) => (
              <tr key={m.user_id}>
                <td>
                  <span className="member-name">
                    <span className="avatar">
                      {m.username[0]?.toUpperCase()}
                    </span>
                    {m.username}
                    {m.user_id === user!.id && <small> (you)</small>}
                  </span>
                </td>
                <td>
                  <div className="chips">
                    {m.roles.map((a) => (
                      <RoleChip
                        key={a.id}
                        role={roles.find((r) => r.id === a.role_id)}
                        suffix={
                          a.scope === "project" ? a.project_name : "Workspace"
                        }
                        onRemove={
                          data.can_manage
                            ? () =>
                                ui.run(async () => {
                                  await remove(`assignments/${a.id}`);
                                  await refresh();
                                  ui.notify("Role removed");
                                })
                            : undefined
                        }
                      />
                    ))}
                  </div>
                </td>
              </tr>
            ))}
            {!data.members.length && (
              <tr>
                <td colSpan={2}>No members yet.</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <p className="note">
        Workspace roles apply to every project. Guests belong to the workspace
        but see only the projects they are given a role in.
      </p>
      {data.can_manage && (
        <>
          <OverrideEditor
            workspaceId={id}
            projects={projects}
            roles={roles}
            members={data.members}
            permissions={catalog.permissions}
          />
          <Suspense fallback={<Loading />}>
            <RecentAudit workspaceId={id} />
          </Suspense>
        </>
      )}
    </>
  );
}
