import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { queries } from "../services/queries";
import { ErrorMessage, Loading, ThemeButton } from "../components/ui";
import { useLiveStatus } from "../providers/LiveProvider";

export function NodeSettingsView() {
  const version = useQuery(queries.version());
  const updates = useQuery(queries.updates());
  const liveStatus = useLiveStatus();
  return (
    <div className="node-settings">
      <section className="panel panel-pad" aria-labelledby="node-info-title">
        <div className="section-head">
          <div>
            <h2 id="node-info-title">Startship node</h2>
            <p className="small muted">The daemon serving this dashboard.</p>
          </div>
          <Link className="button" to="/system/updates">
            Manage updates
          </Link>
        </div>
        {version.isPending ? (
          <Loading />
        ) : version.error ? (
          <ErrorMessage error={version.error} />
        ) : (
          <dl className="kv">
            <dt>Installed version</dt>
            <dd>{version.data.version}</dd>
            <dt>Commit</dt>
            <dd>
              <code>{version.data.commit || "Unknown"}</code>
            </dd>
            <dt>Dashboard connection</dt>
            <dd>
              {liveStatus === "connected"
                ? "Connected"
                : liveStatus === "disconnected"
                  ? "Disconnected"
                  : "Connecting…"}
            </dd>
            {updates.data && (
              <>
                <dt>Self-update</dt>
                <dd>
                  {updates.data.development
                    ? "Unavailable for development builds"
                    : updates.data.self_update
                      ? "Configured"
                      : "Not configured"}
                </dd>
              </>
            )}
          </dl>
        )}
      </section>
      <section className="panel panel-pad" aria-labelledby="appearance-title">
        <div className="section-head">
          <div>
            <h2 id="appearance-title">Dashboard appearance</h2>
            <p className="small muted">
              Choose a light or dark theme for this browser.
            </p>
          </div>
          <ThemeButton />
        </div>
      </section>
    </div>
  );
}
