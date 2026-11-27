import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { useDashboard } from "../layouts/DashboardLayout";
import { useSession } from "../providers/SessionProvider";
import { useUI } from "../providers/UIProvider";
import { request } from "../services/api";
import { ServiceModal, ServicePicker } from "./ServiceUI";
import { AsyncForm, ErrorMessage, Loading } from "./ui";
import "./resourceTransfer.css";

type Kind = "projects" | "apps" | "services" | "volumes";
type Ref = { kind: Kind; id: string };
type Resource = Ref & { name: string };
type Linked = Resource & { allowed: boolean };
type Plan = { linked: Linked[]; plan_id: string };
type Result = { resources: Resource[]; notes: string[] };
type Operation = "copy" | "move";
const createPermission: Record<Kind, string> = {
  projects: "projects.create",
  apps: "apps.manage",
  services: "services.manage",
  volumes: "services.manage",
};
const types: Record<Kind, string> = {
  projects: "Project",
  apps: "App",
  services: "Service",
  volumes: "Volume",
};

function key(resource: Ref) {
  return `${resource.kind}/${resource.id}`;
}

export function ResourceTransfer({
  kind,
  id,
  name,
  workspaceId,
}: Resource & { workspaceId: string }) {
  const [open, setOpen] = useState(false);
  const { workspaces = [] } = useDashboard();
  const can = useSession()?.can;
  const destinations = workspaces.filter(
    (workspace) =>
      workspace.id !== workspaceId && can?.(createPermission[kind], workspace),
  );
  if (destinations.length === 0) return null;
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Transfer
      </button>
      {open && (
        <TransferDialog
          resource={{ kind, id }}
          name={name}
          destinations={destinations}
          close={() => setOpen(false)}
        />
      )}
    </>
  );
}

function TransferDialog({
  resource,
  name,
  destinations,
  close,
}: {
  resource: Ref;
  name: string;
  destinations: ReturnType<typeof useDashboard>["workspaces"];
  close: () => void;
}) {
  const [operation, setOperation] = useState<Operation>("copy");
  const [destination, setDestination] = useState("");
  const [excluded, setExcluded] = useState<string[]>([]);
  const { can } = useSession();
  const ui = useUI();
  const navigate = useNavigate();
  const plan = useQuery({
    queryKey: ["transfer-plan", resource],
    queryFn: () => request<Plan>("transfers/plan", { resource }, "POST"),
    staleTime: 0,
    refetchOnWindowFocus: false,
    retry: false,
  });
  const target = destinations.find((workspace) => workspace.id === destination);
  const data = plan.data;
  const linked = data?.linked ?? [];
  const eligible = (item: Linked) =>
    item.allowed && (!target || can(createPermission[item.kind], target));
  const chosen = linked.filter(
    (item) => eligible(item) && !excluded.includes(key(item)),
  );
  const action = operation === "copy" ? "Copy" : "Move";
  return (
    <ServiceModal
      title={`Transfer ${name}`}
      eyebrow="WORKSPACE TRANSFER"
      onClose={close}
      className="resource-transfer"
    >
      {plan.isPending && <Loading />}
      <ErrorMessage error={plan.error} />
      {data && (
        <AsyncForm
          className="service-form"
          onSubmit={async () => {
            const result = await request<Result>(
              `transfers/${operation}`,
              {
                resource,
                destination_workspace_id: destination,
                plan_id: data.plan_id,
                include: chosen.map(({ kind, id }) => ({ kind, id })),
              },
              "POST",
            );
            const root = result.resources[0];
            const url =
              root.kind === "projects"
                ? `/project/${root.id}`
                : `/workspace/${destination}/${root.kind}${root.kind === "volumes" ? "" : `/${root.id}`}`;
            ui.notify(
              [
                operation === "copy" ? "Resources copied." : "Resources moved.",
                ...result.notes,
              ].join(" "),
            );
            close();
            navigate(url);
          }}
        >
          <ServicePicker
            label="Operation"
            value={operation}
            options={[
              { value: "copy", label: "Copy configuration and selected data" },
              { value: "move", label: "Move existing resources" },
            ]}
            onChange={(value) => setOperation(value as Operation)}
          />
          <ServicePicker
            label="Destination workspace"
            value={destination}
            options={destinations.map((workspace) => ({
              value: workspace.id,
              label: workspace.name,
            }))}
            onChange={setDestination}
          />
          {linked.length > 0 && (
            <fieldset className="transfer-links">
              <legend>Linked resources</legend>
              <p className="small muted">
                {operation === "copy"
                  ? "Include linked resources to keep their connections in the copy."
                  : "Keep linked resources selected to preserve connections. Some connections can’t be split."}
              </p>
              {linked.map((item, index) => (
                <label className="transfer-link" key={item.id || index}>
                  <input
                    type="checkbox"
                    checked={chosen.includes(item)}
                    disabled={!eligible(item)}
                    onChange={(event) =>
                      setExcluded((current) =>
                        event.target.checked
                          ? current.filter((id) => id !== key(item))
                          : [...current, key(item)],
                      )
                    }
                  />
                  <span>{item.name || "Restricted resource"}</span>
                  <span className="small muted">
                    {types[item.kind]}
                    {!eligible(item) && " · Permission required"}
                  </span>
                </label>
              ))}
            </fieldset>
          )}
          {operation === "copy" && (
            <p className="small muted">
              Copies don’t bring over deployments, domains or published ports.
              Stop workloads that use any volumes you’re copying.
            </p>
          )}
          <div className="form-actions">
            <button type="button" onClick={close}>
              Cancel
            </button>
            <button type="submit" className="primary" disabled={!target}>
              {action} resources
            </button>
          </div>
        </AsyncForm>
      )}
    </ServiceModal>
  );
}
