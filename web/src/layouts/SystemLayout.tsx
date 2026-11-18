import { NavLink, Outlet } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { queries } from "../services/queries";
import { Heading } from "../components/Heading";
import { Icon } from "../components/Icon";

export function SystemLayout() {
  const { data } = useQuery(queries.updates());
  return (
    <>
      <Heading
        kicker="System"
        title="Node settings"
        subtitle="Manage this Startship installation and your dashboard preferences."
      />
      <nav className="settings-navigation" aria-label="System navigation">
        <NavLink to="/system" end>
          <Icon name="settings" />
          General
        </NavLink>
        <NavLink to="/system/updates">
          <Icon name="server" />
          Updates
          {data?.update_available && (
            <span className="update-dot" aria-label="Update available" />
          )}
        </NavLink>
      </nav>
      <Outlet />
    </>
  );
}
