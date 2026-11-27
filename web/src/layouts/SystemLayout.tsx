import { NavLink, Outlet, useLocation } from "react-router-dom";
import { useSession } from "../providers/SessionProvider";
import { useQuery } from "@tanstack/react-query";
import { queries } from "../services/queries";
import { Heading } from "../components/Heading";
import { Icon } from "../components/Icon";

export function SystemLayout() {
  const { user, can } = useSession();
  const pathname = useLocation().pathname;
  const network = pathname.endsWith("/network-firewall");
  const migrations = pathname.endsWith("/migrations");
  const { data } = useQuery(queries.updates());
  const title = network
    ? "Network firewall"
    : migrations
      ? "Migrations"
      : "Node settings";
  const subtitle = network
    ? "Host-wide packet filtering for IPv4 and IPv6."
    : migrations
      ? "Import projects, services, volumes, domains, and logs from Openship, Dokploy, or Coolify."
      : "Manage this Startship installation and your dashboard preferences.";
  return (
    <>
      <Heading kicker="System" title={title} subtitle={subtitle} />
      <nav className="settings-navigation" aria-label="System navigation">
        <NavLink to="/system" end>
          <Icon name="settings" />
          General
        </NavLink>
        <NavLink to="/system/updates">
          <Icon name="server" />
          {data?.update_available ? "Update available" : "Updates"}
        </NavLink>
        {can("network.firewall") && (
          <NavLink to="/system/network-firewall">
            <Icon name="shield" />
            Network firewall
          </NavLink>
        )}
        {user?.platform_admin && (
          <NavLink to="/system/migrations">
            <Icon name="download" />
            Migrations
          </NavLink>
        )}
      </nav>
      <Outlet />
    </>
  );
}
