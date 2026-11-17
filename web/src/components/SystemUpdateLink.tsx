import { useQuery } from "@tanstack/react-query";
import { NavLink } from "react-router-dom";
import { queries } from "../services/queries";
import { Icon } from "./Icon";

export function SystemUpdateLink() {
  const { data } = useQuery(queries.updates());
  return (
    <NavLink
      to="/system"
      className="sidebar-selector"
      title="System updates"
      aria-label={
        data?.update_available
          ? "System updates: update available"
          : "System updates"
      }
    >
      <Icon name="server" />
      <span className="selector-name">System updates</span>
      {data?.update_available && (
        <span className="update-dot" aria-hidden="true" />
      )}
    </NavLink>
  );
}
