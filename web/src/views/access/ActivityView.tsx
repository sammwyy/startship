import { Link, useParams, useSearchParams } from "react-router-dom";
import { Heading } from "../../components/Heading";
import { Icon } from "../../components/Icon";
import { ErrorMessage, Loading } from "../../components/ui";
import { useDashboard } from "../../layouts/DashboardLayout";
import { useLiveFeed } from "../../providers/LiveProvider";
import { useSession } from "../../providers/SessionProvider";
import type { LiveSubscription } from "../../services/live";
import type { ActivityEntry } from "../../services/types";
import "./AccountView.css";

const dateFormat = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
});
export function ActivityTime({ value }: { value: string }) {
  const date = new Date(value);
  return (
    <time dateTime={value} title={value}>
      {Number.isNaN(date.valueOf()) ? value : dateFormat.format(date)}
    </time>
  );
}

const actionLabels: Record<string, string> = {
  "session.login": "Signed in",
  "session.logout": "Signed out",
  "session.revoke": "Revoked a session",
  "account.update": "Updated account settings",
  "token.create": "Created an access token",
  "token.revoke": "Revoked an access token",
  "project.deploy": "Queued a deployment",
  "project.redeploy": "Queued a redeployment",
};
const capitalize = (word: string) =>
  word.charAt(0).toUpperCase() + word.slice(1);
function actionLabel(action: string) {
  if (actionLabels[action]) return actionLabels[action];
  const segments = action.split(".");
  const verb = segments.pop();
  return `${capitalize(verb || "Changed")} ${segments.join(" ")}`;
}
function actionIcon(action: string) {
  if (action.startsWith("session.")) return "key";
  if (action.startsWith("token.")) return "plug";
  if (action.endsWith(".delete")) return "trash";
  if (action.endsWith(".create")) return "plus";
  if (/deploy$/.test(action)) return "rocket";
  if (action.startsWith("firewall.")) return "shield";
  return "activity";
}

export function ActivityFeed({
  spec,
  personal = false,
}: {
  spec: LiveSubscription;
  personal?: boolean;
}) {
  const { user } = useSession();
  const [params] = useSearchParams();
  const selectedSession = personal ? params.get("session") : null;
  const feed = useLiveFeed<ActivityEntry[]>(spec);
  if (feed.error) return <ErrorMessage error={feed.error} />;
  if (!feed.data) return <Loading />;
  const entries = feed.data.filter(
    (entry) => !selectedSession || entry.session_id === selectedSession,
  );
  return (
    <section
      className="panel account-panel activity-feed"
      aria-label="Recent activity"
    >
      <header className="account-panel-header">
        <div>
          <h2>Recent activity</h2>
          <p className="small">
            {selectedSession
              ? `Session ${selectedSession.slice(0, 8)}`
              : "Newest events first"}
          </p>
        </div>
        {selectedSession ? (
          <Link className="button" to="/account/activity">
            Show all
          </Link>
        ) : (
          <span
            className="activity-live"
            data-connected={feed.status === "connected"}
          >
            {feed.status === "connected" ? "Live" : "Reconnecting"}
          </span>
        )}
      </header>
      {entries.length === 0 ? (
        <div className="activity-empty">
          <Icon name="history" />
          <strong>No recent activity</strong>
          <p className="small">New actions will appear here as they happen.</p>
        </div>
      ) : (
        <ol className="activity-list">
          {entries.map((entry) => (
            <li key={entry.id}>
              <span className="activity-icon">
                <Icon name={actionIcon(entry.action)} />
              </span>
              <div className="activity-entry">
                <strong>{actionLabel(entry.action)}</strong>
                {!entry.action.startsWith("session.") &&
                  entry.action !== "account.update" && (
                    <span className="activity-target">{entry.target}</span>
                  )}
                <div className="activity-meta">
                  <span>{personal ? "You" : entry.actor || "System"}</span>
                  {personal && entry.session_id && (
                    <>
                      <span aria-hidden="true">·</span>
                      <Link
                        to={`/account/activity?session=${encodeURIComponent(entry.session_id)}`}
                        title={entry.session_id}
                      >
                        {entry.session_id === user?.session_id
                          ? "This session"
                          : `Session ${entry.session_id.slice(0, 8)}`}
                      </Link>
                    </>
                  )}
                  {entry.source === "token" && (
                    <>
                      <span aria-hidden="true">·</span>
                      {personal ? (
                        <Link to="/account/tokens" title={entry.token_id}>
                          Token {entry.token}
                        </Link>
                      ) : (
                        <span>Access token</span>
                      )}
                    </>
                  )}
                  {personal &&
                    !entry.session_id &&
                    entry.source !== "token" && (
                      <>
                        <span aria-hidden="true">·</span>
                        <span>
                          {entry.source === "local_socket"
                            ? "Local CLI"
                            : "Session unavailable"}
                        </span>
                      </>
                    )}
                  {!personal && (
                    <span className="activity-scope">{entry.scope}</span>
                  )}
                </div>
              </div>
              <ActivityTime value={entry.at} />
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

export function ActivityView() {
  const { workspaceId, projectId } = useParams();
  const { workspaces, project } = useDashboard();
  const workspace = workspaces.find((entry) => entry.id === workspaceId);
  const spec: LiveSubscription | null = projectId
    ? { topic: "project_activity", project_id: projectId }
    : workspaceId
      ? { topic: "workspace_activity", workspace_id: workspaceId }
      : null;
  return (
    <div className="account-page">
      <Heading
        title="Activity log"
        kicker={projectId ? "Project" : "Workspace"}
        subtitle={
          projectId
            ? `Recorded actions for ${project?.name || "this project"}.`
            : `Recorded actions across ${workspace?.name || "this workspace"} and its projects.`
        }
      />
      {spec && <ActivityFeed spec={spec} />}
    </div>
  );
}
