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
      title={data?.update_available ? "Update available" : "Node settings"}
      aria-label={
        data?.update_available ? "System: update available" : "System"
      }
    >
      <Icon name="server" />
      <span className="selector-name">
        {data?.update_available ? "System: update available" : "System"}
      </span>
    </NavLink>
  );
}
