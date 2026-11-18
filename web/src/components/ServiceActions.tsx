import type { Service } from "../services/types";
import { remove, request } from "../services/api";
import { useUI } from "../providers/UIProvider";
import { Popover } from "./Popover";
import { Icon } from "./Icon";
export function ServiceActions({
  service,
  onSaved,
}: {
  service: Service;
  onSaved: () => Promise<unknown>;
}) {
  const ui = useUI();
  return (
    <Popover
      id={`service-actions-${service.id}`}
      label={`Actions for ${service.name}`}
      trigger={<span aria-hidden="true">•••</span>}
      className="icon-button service-overflow"
    >
      {["start", "stop", "restart"].map((action) => (
        <button
          key={action}
          disabled={
            (action === "start" && service.status === "running") ||
            (action === "stop" && service.status === "stopped")
          }
          onClick={() =>
            ui.run(async () => {
              await request(`services/${service.id}/${action}`, {});
              await onSaved();
            })
          }
        >
          <Icon
            name={
              action === "stop"
                ? "box"
                : action === "restart"
                  ? "history"
                  : "bolt"
            }
          />
          {action[0].toUpperCase() + action.slice(1)}
        </button>
      ))}
      <button
        className="danger"
        onClick={() =>
          ui.dialog({
            title: `Delete ${service.name}?`,
            description:
              "The service will stop and all project connections will be removed. Its persistent data is kept unless you choose to delete it below.",
            submit: "Delete service",
            fields: (
              <label className="checkbox-line">
                <input type="checkbox" name="delete_volume" />
                Also delete data (permanently)
              </label>
            ),
            onSubmit: async (form) => {
              await remove(
                `services/${service.id}${form.has("delete_volume") ? "?delete_volume=true" : ""}`,
              );
              await onSaved();
            },
          })
        }
      >
        <Icon name="trash" />
        Delete service
      </button>
    </Popover>
  );
}
