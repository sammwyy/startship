import { Heading } from "../../components/Heading";
import { ActivityFeed } from "./ActivityView";
export function AuditView() {
  return (
    <div className="account-page">
      <Heading
        kicker="Platform"
        title="Audit log"
        subtitle="Recorded actions across the platform, including workspace and project changes."
      />
      <ActivityFeed spec={{ topic: "audit_activity" }} />
    </div>
  );
}
