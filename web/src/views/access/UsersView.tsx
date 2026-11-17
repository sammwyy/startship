import { useQueryClient, useSuspenseQueries } from "@tanstack/react-query";
import { queries } from "../../services/queries";
import { remove, request } from "../../services/api";
import type { Assignment } from "../../services/types";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { text } from "../../lib/project";
import { Heading } from "../../components/Heading";
import { Field } from "../../components/ui";
import { Icon } from "../../components/Icon";
import { RoleChip } from "../../components/Access";

export function UsersView() {
  const [{ data: users }, { data: roles }] = useSuspenseQueries({
    queries: [queries.users(), queries.roles()],
  });
  const { user } = useSession(),
    ui = useUI(),
    client = useQueryClient();
  const refresh = () => client.invalidateQueries();
  const password = (
    <Field
      label="Temporary password"
      name="password"
      type="password"
      required
      minLength={8}
      maxLength={72}
      autoComplete="new-password"
    />
  );
  return (
    <>
      <Heading
        kicker="Platform"
        title="Users"
        subtitle="Accounts on this server and the roles they hold across the platform."
        actions={
          <button
            className="primary"
            onClick={() =>
              ui.dialog({
                title: "New user",
                description:
                  "They choose their own password the first time they sign in.",
                submit: "Create user",
                fields: (
                  <>
                    <Field
                      label="Username"
                      name="username"
                      required
                      maxLength={100}
                      autoComplete="off"
                    />
                    {password}
                  </>
                ),
                onSubmit: async (form) => {
                  await request("users", Object.fromEntries(form));
                  await refresh();
                  ui.notify("User created");
                },
              })
            }
          >
            <Icon name="plus" />
            New user
          </button>
        }
      />
      <div className="panel table-panel">
        <table>
          <thead>
            <tr>
              <th>User</th>
              <th>Platform roles</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id}>
                <td>
                  <span className="member-name">
                    <span className="avatar">
                      {u.username[0]?.toUpperCase()}
                    </span>
                    {u.username}
                    {u.id === user!.id && <small> (you)</small>}
                    {u.must_change_password && (
                      <span className="badge queued">Password pending</span>
                    )}
                  </span>
                </td>
                <td>
                  <div className="chips">
                    {u.platform_roles?.map((id) => (
                      <RoleChip
                        key={id}
                        role={roles.find((r) => r.id === id)}
                        onRemove={() =>
                          ui.run(async () => {
                            const assignments = await request<Assignment[]>(
                              `assignments?user_id=${u.id}&scope=platform`,
                            );
                            const assignment = assignments.find(
                              (a) => a.role_id === id,
                            );
                            if (assignment)
                              await remove(`assignments/${assignment.id}`);
                            await refresh();
                          })
                        }
                      />
                    ))}
                    <button
                      className="chip-add"
                      aria-label={`Add a platform role to ${u.username}`}
                      onClick={() =>
                        ui.dialog({
                          title: "Add platform role",
                          description:
                            "Platform roles apply everywhere on this server.",
                          submit: "Add role",
                          fields: (
                            <label>
                              Role
                              <select name="role_id">
                                {roles
                                  .filter(
                                    (r) =>
                                      r.id !== "platform-admin" ||
                                      user?.platform_admin,
                                  )
                                  .map((r) => (
                                    <option key={r.id} value={r.id}>
                                      {r.name}
                                    </option>
                                  ))}
                              </select>
                            </label>
                          ),
                          onSubmit: async (form) => {
                            await request("assignments", {
                              user_id: u.id,
                              role_id: text(form, "role_id"),
                              scope: "platform",
                              scope_id: "",
                            });
                            await refresh();
                          },
                        })
                      }
                    >
                      <Icon name="plus" />
                      Add role
                    </button>
                  </div>
                </td>
                <td className="row-actions">
                  <button
                    onClick={() =>
                      ui.dialog({
                        title: `Reset password for ${u.username}`,
                        description:
                          "Signs them out everywhere; they must choose a new password at the next sign-in.",
                        submit: "Reset password",
                        fields: password,
                        onSubmit: async (form) => {
                          await request(
                            `users/${u.id}`,
                            { password: text(form, "password") },
                            "PATCH",
                          );
                          ui.notify("Password reset");
                        },
                      })
                    }
                  >
                    <Icon name="key" />
                    Reset password
                  </button>
                  {u.id !== user!.id && (
                    <button
                      className="danger"
                      onClick={() =>
                        ui.dialog({
                          title: `Delete ${u.username}`,
                          description:
                            "Removes the account, its sessions and every role it holds. Workspaces and projects stay.",
                          submit: "Delete user",
                          onSubmit: async () => {
                            await remove(`users/${u.id}`);
                            await refresh();
                            ui.notify("User deleted");
                          },
                        })
                      }
                    >
                      <Icon name="trash" />
                      Delete
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="note">
        Users start with no platform permissions: they can only edit their own
        account until a role or a workspace membership gives them more.
      </p>
    </>
  );
}
