import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { request } from "../services/api";
import type { Release } from "../services/types";
import { useUI } from "../providers/UIProvider";
import { Badge, ErrorMessage } from "./ui";
import { Icon } from "./Icon";
import { useLiveStatus, useLiveSubscription } from "../providers/LiveProvider";

export function useLog(releaseId: string, containerId = "") {
  const liveError = useLiveSubscription(
    containerId
      ? { topic: "container_log", container_id: containerId }
      : releaseId
        ? { topic: "release_log", release_id: releaseId }
        : null,
  );
  const path = containerId
    ? `containers/${containerId}/logs`
    : `releases/${releaseId}`;
  const query = useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<Release>(path, undefined, "GET", signal),
    enabled: !!(releaseId || containerId),
  });
  return { ...query, error: liveError || query.error };
}
export function LogPanel({
  log,
  status,
  title,
  error,
  loading = false,
}: {
  log?: string;
  status?: string;
  title?: string;
  error?: unknown;
  loading?: boolean;
}) {
  const [follow, setFollow] = useState(true),
    [wrap, setWrap] = useState(true),
    pre = useRef<HTMLPreElement>(null),
    ui = useUI();
  const liveStatus = useLiveStatus();
  const output = log || "";
  const lineCount = output ? output.trimEnd().split("\n").length : 0;
  const placeholder = loading
    ? "Loading logs…"
    : status && ["queued", "building", "testing", "starting"].includes(status)
      ? "Waiting for build output…"
      : status
        ? "No output was recorded for this deployment."
        : "Deploy your project to see its build output here.";
  useEffect(() => {
    if (follow && pre.current) pre.current.scrollTop = pre.current.scrollHeight;
  }, [follow, output]);
  return (
    <section className="panel log-panel project-log-panel">
      <div className="log-head">
        <div className="log-title">
          <Icon name="terminal" />
          <strong>{title || "Build logs"}</strong>
          {status && <Badge status={status} />}
        </div>
        <div className="log-actions">
          <button
            className="log-toggle"
            aria-pressed={wrap}
            onClick={() => setWrap(!wrap)}
          >
            Wrap
          </button>
          <label className="inline-check">
            <input
              type="checkbox"
              checked={follow}
              onChange={(e) => setFollow(e.target.checked)}
            />
            Follow
          </label>
          <button
            className="icon-button"
            aria-label="Copy logs"
            disabled={!output}
            onClick={() =>
              ui.run(async () => {
                await navigator.clipboard.writeText(output);
                ui.notify("Logs copied");
              })
            }
          >
            <Icon name="copy" />
          </button>
        </div>
      </div>
      <ErrorMessage error={error} />
      <pre
        className={`log${wrap ? "" : " log-no-wrap"}${output ? "" : " log-placeholder"}`}
        ref={pre}
        aria-label="Project logs"
        aria-busy={loading}
      >
        {output || placeholder}
      </pre>
      <div className="log-footer">
        <span>
          {output
            ? `${lineCount} ${lineCount === 1 ? "line" : "lines"}`
            : "No output yet"}
        </span>
        <span>
          {liveStatus !== "connected"
            ? "Live stream reconnecting…"
            : follow
              ? "Following latest output"
              : "Scroll paused"}
        </span>
      </div>
    </section>
  );
}
