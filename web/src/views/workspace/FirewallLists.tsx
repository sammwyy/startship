import { useState } from "react";
import { Icon } from "../../components/Icon";
import { AsyncForm, Field, Empty } from "../../components/ui";
import { useUI } from "../../providers/UIProvider";
import {
  addEntry,
  createList,
  deleteEntry,
  deleteList,
  durationSeconds,
} from "../../services/firewall";
import type { IPList } from "../../services/firewall";
import { Picker, Expiry } from "./FirewallControls";

function CreateList() {
  const [kind, setKind] = useState<IPList["kind"]>("block");
  return (
    <div className="fw-stack">
      <Field label="List name" name="name" required />
      <label>
        List kind
        <Picker
          label="List kind"
          value={kind}
          options={["block", "allow"]}
          onChange={(v) => setKind(v as IPList["kind"])}
        />
        <input type="hidden" name="kind" value={kind} />
      </label>
    </div>
  );
}
export function FirewallLists({
  workspaceId,
  lists,
  canManage,
}: {
  workspaceId: string;
  lists: IPList[];
  canManage: boolean;
}) {
  const ui = useUI();
  const [adding, setAdding] = useState<string>();
  return (
    <div className="fw-stack">
      <div className="fw-section-head">
        <div>
          <h2>IP lists</h2>
          <p>Block unwanted traffic or allow trusted IPs and networks.</p>
        </div>
        {canManage && (
          <button
            className="primary"
            onClick={() =>
              ui.dialog({
                title: "Create IP list",
                fields: <CreateList />,
                submit: "Create list",
                onSubmit: async (f) => {
                  await createList(
                    workspaceId,
                    String(f.get("name")),
                    String(f.get("kind")) as IPList["kind"],
                  );
                  ui.notify("List created");
                },
              })
            }
          >
            <Icon name="plus" />
            Create list
          </button>
        )}
      </div>
      {!lists.length && (
        <Empty title="No IP lists">
          Create a block or allow list to get started.
        </Empty>
      )}
      {[...lists]
        .sort((a, b) => Number(b.id === "banned") - Number(a.id === "banned"))
        .map((list) => (
          <section className="panel fw-list" key={list.id}>
            <div className="fw-section-head fw-panel">
              <div>
                <h3>
                  <Icon name="shield" />
                  {list.name}
                  <span className={`fw-action fw-action-${list.kind}`}>
                    {list.id === "banned" ? "automatic bans" : list.kind}
                  </span>
                </h3>
                <p>
                  {list.id === "banned"
                    ? "Temporary and permanent bans from rules and manual entries."
                    : list.kind === "allow"
                      ? "Bypasses custom and managed rules and workspace rate limits."
                      : "Checked before custom and managed rules."}
                </p>
              </div>
              <div className="fw-inline">
                {canManage && (
                  <>
                    <button
                      onClick={() =>
                        setAdding(adding === list.id ? undefined : list.id)
                      }
                    >
                      <Icon name="plus" />
                      Add entry
                    </button>
                    {list.id !== "banned" && (
                      <button
                        className="icon-button danger"
                        aria-label={`Delete list ${list.name}`}
                        onClick={() =>
                          ui.dialog({
                            title: "Delete IP list",
                            description: `Delete ${list.name} and all its entries?`,
                            submit: "Delete list",
                            onSubmit: async () => {
                              await deleteList(workspaceId, list.id);
                            },
                          })
                        }
                      >
                        <Icon name="trash" />
                      </button>
                    )}
                  </>
                )}
              </div>
            </div>
            {adding === list.id && canManage && (
              <AsyncForm
                className="fw-entry-form"
                onSubmit={async (f, form) => {
                  await addEntry(
                    workspaceId,
                    list.id,
                    String(f.get("value")),
                    durationSeconds(String(f.get("duration"))),
                    String(f.get("note")),
                  );
                  form.reset();
                  setAdding(undefined);
                  ui.notify("Entry added");
                }}
              >
                <div className="fw-form-grid">
                  <Field
                    label="IP address or CIDR"
                    name="value"
                    placeholder="203.0.113.0/24"
                    required
                  />
                  <Field
                    label="Expires after (optional)"
                    name="duration"
                    placeholder="7d — blank is permanent"
                  />
                  <Field
                    label="Note"
                    name="note"
                    placeholder="Office network"
                  />
                </div>
                <div className="form-actions">
                  <button type="button" onClick={() => setAdding(undefined)}>
                    Cancel
                  </button>
                  <button type="submit" className="primary">
                    Add entry
                  </button>
                </div>
              </AsyncForm>
            )}
            {list.entries?.length ? (
              <div className="fw-table">
                <table>
                  <thead>
                    <tr>
                      <th>IP / network</th>
                      <th>Expires</th>
                      <th>Note</th>
                      <th>
                        <span className="sr-only">Actions</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {list.entries.map((entry) => (
                      <tr key={entry.value}>
                        <td>
                          <code>{entry.value}</code>
                        </td>
                        <td>
                          <Expiry value={entry.expires_at} />
                        </td>
                        <td className="fw-wrap">{entry.note || "—"}</td>
                        <td>
                          {canManage && (
                            <button
                              className="fw-text-button danger"
                              aria-label={`${list.id === "banned" ? "Unban" : "Remove"} ${entry.value}`}
                              onClick={() =>
                                ui.run(async () => {
                                  await deleteEntry(
                                    workspaceId,
                                    list.id,
                                    entry.value,
                                  );
                                  ui.notify("Entry removed");
                                })
                              }
                            >
                              {list.id === "banned" ? "Unban" : "Remove"}
                            </button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="fw-empty-row muted">
                No entries.{" "}
                {list.id === "banned"
                  ? "Blocked clients with a ban action will appear here."
                  : "Add an IP address or CIDR to this list."}
              </p>
            )}
          </section>
        ))}
    </div>
  );
}
