import { NavLink, Outlet, useLocation } from "react-router-dom";
import { useSession } from "../providers/SessionProvider";
import { useQuery } from "@tanstack/react-query";
import { queries } from "../services/queries";
import { Heading } from "../components/Heading";
import { Icon } from "../components/Icon";

export function SystemLayout() {
  const { can } = useSession();
  const network = useLocation().pathname.endsWith("/network-firewall");
  const { data } = useQuery(queries.updates());
  return (
    <>
      <Heading
        kicker="System"
        title={network ? "Network firewall" : "Node settings"}
        subtitle={
          network
            ? "Host-wide packet filtering for IPv4 and IPv6."
            : "Manage this Startship installation and your dashboard preferences."
        }
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
        {can("network.firewall") && (
          <NavLink to="/system/network-firewall">
            <Icon name="shield" />
            Network firewall
          </NavLink>
        )}
      </nav>
      <Outlet />
    </>
  );
}
