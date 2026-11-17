import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { request } from "../services/api";
import type { Release } from "../services/types";
import { useUI } from "../providers/UIProvider";
import { Badge, ErrorMessage } from "./ui";
import { Icon } from "./Icon";

export function useLog(releaseId: string, containerId = "") {
  const path = containerId
    ? `containers/${containerId}/logs`
    : `releases/${releaseId}`;
  return useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => request<Release>(path, undefined, "GET", signal),
    enabled: !!(releaseId || containerId),
    refetchInterval: 2000,
  });
}
export function LogPanel({
  log,
  status,
  title,
  error,
}: {
  log?: string;
  status?: string;
  title?: string;
  error?: unknown;
}) {
  const [follow, setFollow] = useState(true),
    pre = useRef<HTMLPreElement>(null),
    ui = useUI();
  const output = log || "Select a release or deploy your project.";
  useEffect(() => {
    if (follow && pre.current) pre.current.scrollTop = pre.current.scrollHeight;
  }, [follow, output]);
  return (
    <section className="panel log-panel project-log-panel">
      <div className="log-head">
        <div className="log-title">
          <Icon name="terminal" />
          <strong>{title || "Project build output"}</strong>
          <Badge status={status} />
        </div>
        <div className="log-actions">
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
      <pre className="log" ref={pre} aria-label="Project logs">
        {output}
      </pre>
    </section>
  );
}
