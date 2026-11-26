export interface UptimeBucket {
  at: string;
  checks: number;
  uptime: number;
}

export interface UptimeIncident {
  start: string;
  end: string | null;
  cause: string;
}

export interface UptimeCheck {
  at: string;
  ok: boolean;
  latency_ms: number;
  error?: string;
}

export interface UptimeSummary {
  state: string;
  uptime_24h: number | null;
  uptime_7d: number | null;
  uptime_30d: number | null;
  history: UptimeBucket[];
  checks: UptimeCheck[];
  incidents: UptimeIncident[];
}

const stateLabels: Record<string, string> = {
  operational: "Operational",
  down: "Outage",
  degraded: "Check failing",
  sleeping: "Sleeping · not checked",
  static: "Static · not checked",
  disabled: "Monitoring off",
  pending: "Awaiting checks",
};

export const uptimePercent = (value: number | null) =>
  value === null ? "—" : `${value.toFixed(2)}%`;

export function UptimeState({ state }: { state: string }) {
  return (
    <span className={`uptime-state uptime-${state}`}>
      {stateLabels[state] ?? "Unknown"}
    </span>
  );
}

export function UptimeHistory({ history }: { history: UptimeBucket[] }) {
  return (
    <>
      <div
        className="uptime-history"
        aria-label={`${history.length}-day uptime history`}
      >
        {history.map(({ at, checks, uptime }) => {
          const label = `${at.slice(0, 10)} UTC · ${checks ? `${uptimePercent(uptime)} uptime · ${checks} checks` : "No checks"}`;
          return (
            <span
              key={at}
              role="img"
              aria-label={label}
              title={label}
              className={`uptime-bar ${!checks ? "" : uptime === 100 ? "uptime-operational" : uptime === 0 ? "uptime-down" : "uptime-degraded"}`}
            />
          );
        })}
      </div>
      <div className="uptime-axis">
        <span>{history.length} days incl. today</span>
        <span>Today · UTC</span>
      </div>
    </>
  );
}

const dateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
});

function formatDuration(start: string, end: string | null) {
  const seconds = Math.max(
    0,
    Math.floor(
      ((end ? Date.parse(end) : Date.now()) - Date.parse(start)) / 1000,
    ),
  );
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  return minutes < 60
    ? `${minutes}m`
    : `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

export function UptimeIncidents({
  incidents,
}: {
  incidents: UptimeIncident[];
}) {
  if (!incidents.length)
    return <p className="uptime-empty">No incidents recorded.</p>;
  return (
    <ul className="uptime-incidents">
      {incidents.map(({ start, end, cause }) => (
        <li key={start}>
          <div>
            <strong>{end ? "Resolved" : "Ongoing incident"}</strong>
            <p>{cause}</p>
          </div>
          <div>
            <span>
              Start ·{" "}
              <time dateTime={start}>{dateTime.format(new Date(start))}</time>
            </span>
            <span>
              End ·{" "}
              {end ? (
                <time dateTime={end}>{dateTime.format(new Date(end))}</time>
              ) : (
                "Ongoing"
              )}
            </span>
          </div>
          <span>{formatDuration(start, end)}</span>
        </li>
      ))}
    </ul>
  );
}

export function UptimeLatency({ checks }: { checks: UptimeCheck[] }) {
  if (!checks.length)
    return (
      <p className="uptime-empty">
        Response times will appear after the first check.
      </p>
    );
  const samples = [...checks].reverse();
  const max = Math.max(1, ...samples.map((check) => check.latency_ms));
  return (
    <>
      <div className="uptime-latency">
        {samples.map((check) => {
          const label = `${dateTime.format(new Date(check.at))} · ${check.latency_ms.toFixed(1)} ms · ${check.ok ? "Passed" : "Failed"}`;
          return (
            <span
              key={check.at}
              role="img"
              aria-label={label}
              title={label}
              className={check.ok ? "" : "uptime-down"}
              style={{
                height: `${Math.max(4, (check.latency_ms / max) * 100)}%`,
              }}
            />
          );
        })}
      </div>
      <div className="uptime-axis">
        <span>Oldest check</span>
        <span>Latest · {checks[0].latency_ms.toFixed(1)} ms</span>
      </div>
    </>
  );
}
