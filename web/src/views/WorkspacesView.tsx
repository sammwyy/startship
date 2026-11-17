import { Link, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { useDashboard } from "../layouts/DashboardLayout";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { request } from "../services/api";
import type { Workspace } from "../services/types";
import { queries } from "../services/queries";
import { Heading } from "../components/Heading";
import { Icon } from "../components/Icon";
import { Empty, Field } from "../components/ui";
import { text } from "../lib/project";

export function WorkspacesView() {
  const { workspaces, workspace } = useDashboard(),
    { can } = useSession(),
    ui = useUI(),
    client = useQueryClient(),
    navigate = useNavigate();
  const create = () =>
    ui.dialog({
      title: "Create workspace",
      description: "A home for your projects.",
      submit: "Create workspace",
      fields: <Field label="Workspace name" name="name" required />,
      onSubmit: async (form) => {
        const w = await request<Workspace>("workspaces", {
          name: text(form, "name"),
        });
        await client.invalidateQueries(queries.workspaces());
        navigate(`/workspace/${w.id}`);
        ui.notify("Workspace created");
      },
    });
  return (
    <>
      <Heading
        kicker="Infrastructure"
        title="Workspaces"
        subtitle="Organize the projects running on your server."
        actions={
          can("workspaces.create") && (
            <button className="primary" onClick={create}>
              <Icon name="plus" />
              New workspace
            </button>
          )
        }
      />
      <div className="section-head">
        <h2>
          All workspaces <span className="count">{workspaces.length}</span>
        </h2>
      </div>
      {workspaces.length ? (
        <div className="grid">
          {workspaces.map((w) => (
            <Link className="card link" key={w.id} to={`/workspace/${w.id}`}>
              <div className="card-top">
                <div className="card-icon">
                  <Icon name="layers" />
                </div>
                {w.id === workspace?.id ? (
                  <span className="badge running">Current</span>
                ) : (
                  <Icon name="arrow" />
                )}
              </div>
              <h3>{w.name}</h3>
              <div className="card-footer">
                <span>
                  {w.created
                    ? `Created ${new Date(w.created).toLocaleDateString()}`
                    : "Workspace"}
                </span>
                <span>
                  Open workspace <Icon name="arrow" />
                </span>
              </div>
            </Link>
          ))}
        </div>
      ) : (
        <Empty title="No workspaces yet">
          Create a workspace to organize your projects.
        </Empty>
      )}
    </>
  );
}
