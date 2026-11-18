import { NavLink } from "react-router-dom";
import { useSession } from "../providers/SessionProvider";
import type { Scoped } from "../services/types";
import { Icon } from "./Icon";

const sections = {
  workspace: { home: "Projects", path: "", activity: "Audit Activity" },
  project: { home: "Overview", path: "/overview", activity: "Activity Log" },
};
type Scope = keyof typeof sections;
export const scopedActivityTitle = (scope: Scope) => sections[scope].activity;

export function ScopedActivityTabs({
  scope,
  resource,
}: {
  scope: Scope;
  resource?: Scoped & { id: string };
}) {
  const { can } = useSession();
  if (!resource || !can("activity.view", resource)) return null;
  const base = `/${scope}/${resource.id}`;
  const section = sections[scope];
  return (
    <nav
      className="settings-navigation scoped-activity-tabs"
      aria-label={scope === "workspace" ? "Workspace tabs" : "Project tabs"}
    >
      <NavLink to={base + section.path} end>
        <Icon name="grid" />
        {section.home}
      </NavLink>
      <NavLink to={`${base}/activity`} end>
        <Icon name="history" />
        {section.activity}
      </NavLink>
    </nav>
  );
}
