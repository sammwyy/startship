import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQueryClient, useSuspenseQueries } from "@tanstack/react-query";
import { queries } from "../../services/queries";
import { remove, request } from "../../services/api";
import type { Permission, Role } from "../../services/types";
import { useSession } from "../../providers/SessionProvider";
import { useUI } from "../../providers/UIProvider";
import { Heading } from "../../components/Heading";
import { AsyncForm, Empty } from "../../components/ui";
import { Icon } from "../../components/Icon";
import { groupPermissions, RoleIcon, roleStyle } from "../../components/Access";

const colors = [
  "#e5484d",
  "#f76b15",
  "#f5a524",
  "#30a46c",
  "#12a594",
  "#3e63dd",
  "#7c5cff",
  "#d6409f",
  "#8b8d98",
  "#a18072",
];
function RoleEditor({
  role,
  permissions,
  icons,
}: {
  role: Role;
  permissions: Permission[];
  icons: string[];
}) {
  const { can, user } = useSession(),
    ui = useUI(),
    client = useQueryClient(),
    navigate = useNavigate();
  const [name, setName] = useState(role.name),
    [color, setColor] = useState(role.color),
    [shape, setShape] = useState(role.icon),
    [selected, setSelected] = useState(role.permissions);
  const editable = !role.builtin && can("roles.manage");
  const notice = role.fixed
    ? "Platform Admin holds every permission everywhere. It cannot be edited or deleted, and at least one user always keeps it."
    : role.builtin
      ? "Built-in roles are read-only. Clone one to start a custom role from its permissions."
      : !editable
        ? "You need the Manage roles permission to change roles."
        : "";
  return (
    <AsyncForm
      className="panel role-editor"
      disabled={!editable}
      onSubmit={async () => {
        await request(
          `roles/${role.id}`,
          { name, color, icon: shape, permissions: selected },
          "PATCH",
        );
        await client.invalidateQueries(queries.roles());
        ui.notify("Role saved");
      }}
    >
      <div className="role-preview">
        <RoleIcon role={{ color, icon: shape }} />
        <div>
          <strong>{name || "Untitled role"}</strong>
          <small>
            {role.fixed
              ? "Fixed role"
              : role.builtin
                ? "Built-in role"
                : "Custom role"}{" "}
            · {role.members} members
          </small>
        </div>
      </div>
      {notice && (
        <div className="note role-note">
          <Icon name="lock" />
          {notice}
        </div>
      )}
      <div className="role-fields">
        <label>
          Role name
          <input
            name="name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={60}
            required
          />
        </label>
        <div className="role-field">
          <span className="field-label">Color</span>
          <div className="swatches" role="radiogroup" aria-label="Role color">
            {colors.map((c) => (
              <button
                key={c}
                type="button"
                className="swatch"
                role="radio"
                style={roleStyle(c)}
                aria-checked={c === color}
                aria-label={c}
                onClick={() => setColor(c)}
              />
            ))}
            <label className="swatch custom-color">
              <input
                type="color"
                value={color}
                aria-label="Custom color"
                onChange={(e) => setColor(e.target.value)}
              />
            </label>
          </div>
        </div>
        <div className="role-field">
          <span className="field-label">Icon</span>
          <div className="icon-picker" role="radiogroup" aria-label="Role icon">
            {icons.map((icon) => (
              <button
                key={icon}
                type="button"
                role="radio"
                aria-checked={shape === icon}
                aria-label={icon}
                onClick={() => setShape(icon)}
              >
                <Icon name={icon} />
              </button>
            ))}
          </div>
        </div>
      </div>
      <div className="permission-head">
        <h2>Permissions</h2>
        <span className="count">
          {selected.length}/{permissions.length}
        </span>
      </div>
      {groupPermissions(permissions).map(([group, items]) => (
        <fieldset className="permission-group" key={group}>
          <legend>{group}</legend>
          {items.map((p) => (
            <label className="permission-row" key={p.key}>
              <span>
                <strong>{p.label}</strong>
                <small>{p.description}</small>
              </span>
              <input
                type="checkbox"
                role="switch"
                className="switch"
                disabled={p.scope === "platform" && !user?.platform_admin}
                checked={selected.includes(p.key)}
                onChange={(e) =>
                  setSelected(
                    e.target.checked
                      ? [...selected, p.key]
                      : selected.filter((key) => key !== p.key),
                  )
                }
              />
            </label>
          ))}
        </fieldset>
      ))}
      {editable && (
        <div className="role-actions">
          <button
            type="button"
            className="danger"
            onClick={() =>
              ui.dialog({
                title: `Delete ${role.name}`,
                description: `This removes the role from ${role.members} members and drops its overrides. This cannot be undone.`,
                submit: "Delete role",
                onSubmit: async () => {
                  await remove(`roles/${role.id}`);
                  await client.invalidateQueries(queries.roles());
                  navigate("/roles");
                  ui.notify("Role deleted");
                },
              })
            }
          >
            <Icon name="trash" />
            Delete role
          </button>
          <button className="primary">Save changes</button>
        </div>
      )}
    </AsyncForm>
  );
}
export function RolesView() {
  const [{ data: roles }, { data: catalog }] = useSuspenseQueries({
    queries: [queries.roles(), queries.permissions()],
  });
  const { roleId } = useParams(),
    { can } = useSession(),
    ui = useUI(),
    client = useQueryClient(),
    navigate = useNavigate();
  const role = roles.find((r) => r.id === roleId) || roles[0];
  async function create(clone = false) {
    const next = await request<Role>(
      "roles",
      clone ? { clone_of: role.id } : { name: "New role" },
    );
    await client.invalidateQueries(queries.roles());
    navigate(`/roles/${next.id}`);
    ui.notify("Role created");
  }
  if (!role) return <Empty title="No roles">No roles are available.</Empty>;
  return (
    <>
      <Heading
        kicker="Platform"
        title="Roles"
        subtitle="Name a role, pick a look and switch on what it can do."
        actions={
          can("roles.manage") && (
            <>
              <button onClick={() => ui.run(() => create(true))}>
                <Icon name="copy" />
                Clone
              </button>
              <button
                className="primary"
                onClick={() => ui.run(() => create())}
              >
                <Icon name="plus" />
                New role
              </button>
            </>
          )
        }
      />
      <div className="role-layout">
        <nav className="panel role-list" aria-label="Roles">
          {roles.map((r) => (
            <Link
              key={r.id}
              to={`/roles/${r.id}`}
              aria-current={r.id === role.id ? "page" : undefined}
            >
              <RoleIcon role={r} />
              <span>
                <strong>{r.name}</strong>
                <small>
                  {r.members} members{r.builtin ? " · Built-in" : ""}
                </small>
              </span>
              {r.fixed && <Icon name="lock" />}
            </Link>
          ))}
        </nav>
        <RoleEditor
          key={role.id}
          role={role}
          permissions={catalog.permissions}
          icons={catalog.icons}
        />
      </div>
    </>
  );
}
