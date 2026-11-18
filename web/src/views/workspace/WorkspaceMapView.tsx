import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { useDashboard } from "../../layouts/DashboardLayout";
import { queries } from "../../services/queries";
import { useServices, useVolumes } from "../../services/services";
import {
  useLiveProjects,
  useLiveStatus,
  useLiveSubscription,
} from "../../providers/LiveProvider";
import { sortReleases } from "../../lib/project";
import { buildWorkspaceMap, connectedMapNodes, mapKey } from "../../lib/map";
import type { MapKind } from "../../lib/map";
import { Heading } from "../../components/Heading";
import { Empty, ErrorMessage, Loading } from "../../components/ui";
import { Icon } from "../../components/Icon";
import { EngineIcon } from "../../components/EngineIcon";
import { volumeSize } from "../../components/VolumesPanel";
import "./WorkspaceMapView.css";

type Node = {
  key: string;
  name: string;
  detail: string;
  status: string;
  to: string;
  engine?: string;
};
type Curve = {
  from: string;
  to: string;
  label?: string;
  d: string;
  x: number;
  y: number;
  room: boolean;
};
const tone = (status: string) =>
  ["running", "success", "active"].includes(status)
    ? "running"
    : [
          "building",
          "deploying",
          "pending",
          "queued",
          "testing",
          "starting",
        ].includes(status)
      ? "busy"
      : ["failed", "error"].includes(status)
        ? "failed"
        : "idle";

export function WorkspaceMapView() {
  const id = useParams().workspaceId!,
    { workspace } = useDashboard();
  const [projects, containers, releases] = useQueries({
    queries: [queries.projects(id), queries.containers(), queries.releases()],
  });
  const services = useServices(id),
    volumes = useVolumes(id);
  const liveError = useLiveSubscription({
    topic: "workspace",
    workspace_id: id,
  });
  const liveStatus = useLiveStatus();
  useLiveProjects(projects.data?.map((p) => p.id) || []);
  const graph = useMemo(
    () =>
      buildWorkspaceMap(
        projects.data || [],
        services.data?.services || [],
        volumes.data?.volumes || [],
      ),
    [projects.data, services.data, volumes.data],
  );
  const columns = useMemo(() => {
    const sorted = sortReleases(releases.data || []);
    const projectNodes: Node[] = graph.projects.map((p) => {
      const owned = (containers.data || []).filter(
        (c) => c.project_id === p.id && c.active_deployment,
      );
      const status =
        sorted.find((r) => r.project_id === p.id)?.status ||
        (owned.some((c) => c.status === "running") ? "running" : "idle");
      return {
        key: mapKey("projects", p.id),
        name: p.name,
        detail: `${owned.length} container${owned.length === 1 ? "" : "s"} · ${status}`,
        status,
        to: `/project/${p.id}`,
      };
    });
    const serviceNodes: Node[] = graph.services.map((s) => ({
      key: mapKey("services", s.id),
      name: s.name,
      detail: `${s.engine} · ${s.version}`,
      status: s.status,
      engine: s.engine,
      to: `/workspace/${id}/services/${s.id}`,
    }));
    const volumeNodes: Node[] = graph.volumes.map((v) => {
      const owners = [
        ...projectNodes.filter(
          (p) => p.key === mapKey("projects", v.project_id),
        ),
        ...serviceNodes.filter((s) =>
          graph.edges.some(
            (e) => e.from === s.key && e.to === mapKey("volumes", v.id),
          ),
        ),
      ];
      return {
        key: mapKey("volumes", v.id),
        name: v.name,
        detail: v.size_updated ? volumeSize(v.size_bytes) : "Size pending",
        status: owners.some((n) => tone(n.status) === "running")
          ? "running"
          : "idle",
        to: `/workspace/${id}/volumes`,
      };
    });
    return {
      projects: projectNodes,
      services: serviceNodes,
      volumes: volumeNodes,
    };
  }, [graph, containers.data, releases.data, id]);
  const root = useRef<HTMLDivElement>(null);
  const [curves, setCurves] = useState<Curve[]>([]);
  const [hovered, setHovered] = useState<string | null>(null),
    [focused, setFocused] = useState<string | null>(null);
  const active = focused || hovered;
  const connected = active ? connectedMapNodes(active, graph.edges) : null;
  const nodes = Object.values(columns).flat();
  useLayoutEffect(() => {
    const element = root.current;
    if (!element) return;
    const measure = () => {
      const bounds = element.getBoundingClientRect();
      const anchors = new Map(
        Array.from(element.querySelectorAll<HTMLElement>("[data-map-key]")).map(
          (node) => [node.dataset.mapKey!, node.getBoundingClientRect()],
        ),
      );
      setCurves(
        graph.edges.flatMap((edge) => {
          const a = anchors.get(edge.from),
            b = anchors.get(edge.to);
          if (!a || !b) return [];
          const x1 = a.right - bounds.left,
            x2 = b.left - bounds.left,
            y1 = a.top + a.height / 2 - bounds.top,
            y2 = b.top + b.height / 2 - bounds.top;
          const bend = (x2 - x1) * 0.5;
          return [
            {
              ...edge,
              d: `M${x1},${y1} C${x1 + bend},${y1} ${x2 - bend},${y2} ${x2},${y2}`,
              x: (x1 + x2) / 2,
              y: (y1 + y2) / 2,
              room: x2 - x1 > 100,
            },
          ];
        }),
      );
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    element
      .querySelectorAll("[data-map-key]")
      .forEach((node) => observer.observe(node));
    return () => observer.disconnect();
  }, [graph, columns]);
  const error =
    projects.error ||
    containers.error ||
    releases.error ||
    services.error ||
    volumes.error ||
    liveError ||
    services.liveError ||
    volumes.liveError;
  const pending = [projects, containers, releases, services, volumes].some(
    (q) => q.isPending,
  );
  return (
    <div className="workspace-map">
      <Heading
        title="Map"
        kicker={workspace?.name}
        subtitle="A live view of your projects, their services and the data that connects them."
      />
      <ErrorMessage error={error} />
      {pending ? (
        <Loading />
      ) : !nodes.length ? (
        !error && (
          <Empty title="Your workspace, connected">
            Projects, services and volumes will appear here as you create them.
          </Empty>
        )
      ) : (
        <>
          <div className="map-meta">
            <span>
              {graph.projects.length} projects · {graph.services.length}{" "}
              services · {graph.volumes.length} volumes
            </span>
            <span className="map-live">
              <i
                className={
                  liveStatus === "connected" ? "live-dot" : "map-dot idle"
                }
              />
              {liveStatus === "connected"
                ? "Live updates"
                : liveStatus === "disconnected"
                  ? "Updates disconnected"
                  : "Connecting…"}
            </span>
          </div>
          <div className="map-board" ref={root}>
            <svg className="map-edges" aria-hidden="true">
              <g>
                {curves.map((edge, i) => {
                  const live = [edge.from, edge.to].every(
                    (key) =>
                      tone(nodes.find((n) => n.key === key)?.status || "") ===
                      "running",
                  );
                  const dim = connected && !connected.has(edge.from);
                  return (
                    <g
                      key={i}
                      className={`${live ? "is-live" : ""} ${dim ? "is-dimmed" : ""}`}
                    >
                      <path d={edge.d} />
                      {edge.label && edge.room && (
                        <g
                          className="map-edge-label"
                          transform={`translate(${edge.x},${edge.y})`}
                        >
                          <rect
                            x={-Math.min(edge.label.length * 3 + 9, 67)}
                            y="-10"
                            width={Math.min(edge.label.length * 6 + 18, 134)}
                            height="20"
                            rx="10"
                          />
                          <text textAnchor="middle" dominantBaseline="central">
                            {edge.label.length > 19
                              ? `${edge.label.slice(0, 18)}…`
                              : edge.label}
                          </text>
                        </g>
                      )}
                    </g>
                  );
                })}
              </g>
            </svg>
            {(["projects", "services", "volumes"] as MapKind[]).map((kind) => (
              <section className="map-column" key={kind} aria-label={kind}>
                <h2>
                  {kind}
                  <span>{columns[kind].length}</span>
                </h2>
                <div className="map-node-list">
                  {columns[kind].map((node) => (
                    <Link
                      key={node.key}
                      data-map-key={node.key}
                      to={node.to}
                      className={`map-node ${connected && !connected.has(node.key) ? "is-dimmed" : ""} ${connected?.has(node.key) ? "is-connected" : ""}`}
                      onMouseEnter={() => setHovered(node.key)}
                      onMouseLeave={() => setHovered(null)}
                      onFocus={() => setFocused(node.key)}
                      onBlur={() => setFocused(null)}
                      title={`${node.name} · ${node.detail} · ${node.status}`}
                    >
                      {node.engine ? (
                        <EngineIcon engine={node.engine} />
                      ) : (
                        <span className="map-node-icon">
                          <Icon
                            name={kind === "projects" ? "grid" : "hard-drive"}
                          />
                        </span>
                      )}
                      <span className="map-node-copy">
                        <strong>{node.name}</strong>
                        <small>{node.detail}</small>
                      </span>
                      <i
                        className={`map-dot ${tone(node.status)}`}
                        aria-label={node.status}
                      />
                    </Link>
                  ))}
                  {!columns[kind].length && (
                    <p className="map-column-empty">No {kind} yet</p>
                  )}
                </div>
              </section>
            ))}
          </div>
          <div className="map-legend" aria-label="Status legend">
            {[
              ["running", "Running"],
              ["busy", "Deploying"],
              ["failed", "Failed"],
              ["idle", "Idle"],
            ].map(([status, label]) => (
              <span key={status}>
                <i className={`map-dot ${status}`} />
                {label}
              </span>
            ))}
            <span className="map-hint">
              Hover or focus to trace connections
            </span>
          </div>
        </>
      )}
    </div>
  );
}
