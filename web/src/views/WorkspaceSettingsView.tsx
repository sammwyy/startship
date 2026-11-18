import { useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { useDashboard } from "../layouts/DashboardLayout";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { request } from "../services/api";
import { queries } from "../services/queries";
import { text } from "../lib/project";
import { Heading } from "../components/Heading";
import { AsyncForm, Field } from "../components/ui";

export function WorkspaceSettingsView() {
  const id = useParams().workspaceId!,
    { workspace } = useDashboard(),
    { can } = useSession(),
    ui = useUI(),
    client = useQueryClient();
  return (
    <>
      <Heading
        kicker="Workspace"
        title="Workspace settings"
        subtitle="A shared home for your projects."
      />
      <AsyncForm
        key={workspace?.id + (workspace?.name || "")}
        className="panel panel-pad workspace-settings"
        disabled={!can("workspace.manage", workspace)}
        onSubmit={async (form) => {
          await request(
            `workspaces/${id}`,
            { name: text(form, "name") },
            "PATCH",
          );
          await client.invalidateQueries(queries.workspaces());
          ui.notify("Workspace saved");
        }}
      >
        <Field
          label="Workspace name"
          name="name"
          defaultValue={workspace?.name}
          required
        />
        <button className="primary">Save changes</button>
      </AsyncForm>
    </>
  );
}
