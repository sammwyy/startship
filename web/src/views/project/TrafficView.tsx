import { useState } from "react";
import {
  useLiveStatus,
  useLiveSubscription,
} from "../../providers/LiveProvider";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useProject } from "../../layouts/ProjectLayout";
import { request } from "../../services/api";
import type { Traffic } from "../../services/types";
import { ProjectHeading } from "../../components/ProjectHeading";
import { Icon } from "../../components/Icon";
import { ErrorMessage, Loading } from "../../components/ui";
import {
  TrafficBars,
  TrafficLine,
  trafficMemory,
  trafficMS,
  trafficNumber,
  trafficTime,
} from "../../components/TrafficCharts";

function Metric({
  label,
  value,
  unit,
  children,
  errors,
}: {
  label: string;
  value: ReactNode;
  unit?: ReactNode;
  children: ReactNode;
  errors?: boolean;
}) {
  return (
    <div className={`traffic-metric ${errors ? "has-errors" : ""}`}>
      <span>{label}</span>
      <strong>
        {value}
        <small>{unit}</small>
      </strong>
      <p>{children}</p>
    </div>
  );
}
export function TrafficContent({ data }: { data: Traffic }) {
  const total = data.total,
    count = total.requests,
    good = count - total.errors - total.client_errors;
  const resourceOK = data.resource_status === "ok",
    cpu = data.containers.reduce((n, c) => n + c.cpu_percent, 0),
    memory = data.containers.reduce((n, c) => n + c.memory_bytes, 0);
  const notes: Record<string, string> = {
    waiting: "Waiting for first sample",
    none: "No process resource samples",
    unavailable: "Container metrics unavailable",
    stale: "Container metrics are stale",
  };
  const resourceNote =
    notes[data.resource_status] ||
    `Sampled ${trafficTime(data.sampled_at)} · CPU: 100% = one core`;
  const latest = data.recent.slice(0, 6);
  return (
    <>
      <div className="traffic-summary">
        <Metric label="Requests" value={trafficNumber(count)}>
          {count
            ? "Through your project’s router"
            : "Waiting for the first request"}
        </Metric>
        <Metric
          label="Server errors"
          value={trafficNumber(total.errors)}
          unit={
            <span className="traffic-rate">
              {(count ? (total.errors / count) * 100 : 0).toFixed(1)}%
            </span>
          }
          errors={!!total.errors}
        >
          HTTP 5xx · {trafficNumber(total.client_errors)} client errors (4xx)
        </Metric>
        <Metric
          label="Avg. response time"
          value={count ? trafficMS(total.average_ms) : "—"}
          unit={count ? "ms" : ""}
        >
          Router → complete response
        </Metric>
        <Metric
          label="Container memory"
          value={resourceOK ? trafficMemory(memory).split(" ")[0] : "—"}
          unit={resourceOK ? trafficMemory(memory).split(" ")[1] : ""}
        >
          {resourceOK
            ? `CPU ${cpu.toFixed(1)}% · ${data.containers.length} running`
            : resourceNote}
        </Metric>
      </div>
      <div className="traffic-top-grid">
        <section className="traffic-panel">
          <div className="traffic-panel-head">
            <div>
              <h2>Request volume</h2>
              <span>{data.interval_seconds}-second intervals</span>
            </div>
            <div className="traffic-legend">
              <span>
                <i />
                Requests
              </span>
              <span>
                <i className="errors" />
                5xx errors
              </span>
            </div>
          </div>
          <TrafficBars data={data} />
          <div
            className="traffic-distribution"
            aria-label="HTTP response distribution"
          >
            <span style={{ flex: Math.max(good, 0) }} />
            <span className="client" style={{ flex: total.client_errors }} />
            <span className="errors" style={{ flex: total.errors }} />
          </div>
          <div className="traffic-status-key">
            <span>
              <i />
              2xx / 3xx <strong>{trafficNumber(good)}</strong>
            </span>
            <span>
              <i className="client" />
              4xx <strong>{trafficNumber(total.client_errors)}</strong>
            </span>
            <span>
              <i className="errors" />
              5xx <strong>{trafficNumber(total.errors)}</strong>
            </span>
          </div>
        </section>
        <section className="traffic-panel">
          <div className="traffic-panel-head">
            <div>
              <h2>Response time</h2>
              <span>Average per interval</span>
            </div>
            <span className="traffic-unit">ms</span>
          </div>
          <div className="traffic-latency-number">
            {count ? trafficMS(total.average_ms) : "—"}
            <small>{count ? "ms" : ""}</small>
            <span>
              avg / <b>{count ? `${trafficMS(total.max_ms)} ms` : "—"}</b>{" "}
              slowest
            </span>
          </div>
          <TrafficLine
            data={data}
            metric="average_ms"
            label="Response time in milliseconds"
            kind="latency"
          />
        </section>
      </div>
      <div className="traffic-bottom-grid">
        <section className="traffic-panel traffic-resources">
          <div className="traffic-panel-head">
            <div>
              <h2>Container resources</h2>
              <span>{resourceNote}</span>
            </div>
            <Icon name="box" />
          </div>
          <div className="traffic-resource-grid">
            <div>
              <span className="traffic-kicker">CPU</span>
              <strong>{resourceOK ? `${cpu.toFixed(1)}%` : "—"}</strong>
              <TrafficLine
                data={data}
                metric="cpu_percent"
                label="CPU percent, 100 percent is one core"
                kind="cpu"
              />
            </div>
            <div>
              <span className="traffic-kicker">MEMORY</span>
              <strong>{resourceOK ? trafficMemory(memory) : "—"}</strong>
              <TrafficLine
                data={data}
                metric="memory_bytes"
                label="Container memory in MiB"
                kind="memory"
              />
            </div>
          </div>
          <div className="traffic-container-list">
            {resourceOK ? (
              data.containers.map((c) => (
                <div key={c.name}>
                  <span>
                    <Icon name="box" />
                    {c.name}
                  </span>
                  <span>{c.cpu_percent.toFixed(1)}% CPU</span>
                  <span>
                    {trafficMemory(c.memory_bytes)} /{" "}
                    {trafficMemory(c.limit_bytes)}
                  </span>
                </div>
              ))
            ) : (
              <p>{resourceNote}. Request monitoring continues independently.</p>
            )}
          </div>
        </section>
        <section className="traffic-panel traffic-recent">
          <div className="traffic-panel-head">
            <div>
              <h2>Recent requests</h2>
              <span>Matched route only · no query strings or client data</span>
            </div>
            <span className="traffic-unit">LATEST {latest.length}</span>
          </div>
          <div className="traffic-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Method / route</th>
                  <th>Status</th>
                  <th>Duration</th>
                </tr>
              </thead>
              <tbody>
                {latest.map((r, i) => (
                  <tr key={`${r.at}-${i}`}>
                    <td>{trafficTime(r.at)}</td>
                    <td>
                      <span className="traffic-method">{r.method}</span>
                      <span className="traffic-route" title={r.route}>
                        {r.route}
                      </span>
                    </td>
                    <td>
                      <span
                        className={`traffic-http ${r.status >= 500 ? "server-error" : r.status >= 400 ? "client-error" : ""}`}
                      >
                        {r.status}
                      </span>
                    </td>
                    <td>
                      {trafficMS(r.duration_ms)}
                      <span className="traffic-unit"> ms</span>
                    </td>
                  </tr>
                ))}
                {!latest.length && (
                  <tr>
                    <td colSpan={4} className="traffic-empty">
                      Requests appear here automatically when a project route
                      receives traffic.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </>
  );
}
export function TrafficView() {
  const { project } = useProject(),
    [windowSeconds, setWindow] = useState(300);
  const liveError = useLiveSubscription({
    topic: "traffic",
    project_id: project.id,
    window: windowSeconds,
  });
  const liveStatus = useLiveStatus();
  const query = useQuery({
    queryKey: ["traffic", project.id, windowSeconds],
    queryFn: ({ signal }) =>
      request<Traffic>(
        `projects/${project.id}/traffic?window=${windowSeconds}`,
        undefined,
        "GET",
        signal,
      ),
  });
  const offline = query.isError || liveError || liveStatus !== "connected";
  return (
    <>
      <ProjectHeading title="Traffic" />
      <section className="traffic-page">
        <div className="traffic-toolbar">
          <div>
            {(offline || query.isFetching) && (
              <span className={`traffic-live${offline ? " offline" : ""}`}>
                {query.isError || liveError
                  ? "Disconnected"
                  : query.isFetching
                    ? "Updating…"
                    : "Reconnecting…"}
              </span>
            )}
          </div>
          <div className="traffic-window" aria-label="Traffic time range">
            {[
              [300, "5 minutes"],
              [900, "15 minutes"],
              [3600, "1 hour"],
            ].map(([value, label]) => (
              <button
                key={value}
                aria-pressed={windowSeconds === value}
                onClick={() => setWindow(Number(value))}
              >
                {label}
              </button>
            ))}
          </div>
        </div>
        <ErrorMessage error={query.error} />
        {query.data ? (
          <TrafficContent data={query.data} />
        ) : query.isPending ? (
          <Loading layout="stats" label="Loading traffic…" />
        ) : null}
        <div className="traffic-footnote">
          <span>
            <Icon name="lock" />
            No agents. No SDKs. Nothing to configure.
          </span>
          <span>Last hour · in memory · resets on restart</span>
        </div>
      </section>
    </>
  );
}
