import { useLayoutEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties } from "react";
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
import {
  buildWorkspaceMap,
  connectedMapNodes,
  mapKey,
  placeMapLabels,
  orderMapRow,
  mapLabelPositions,
  sampleMapCurve,
} from "../../lib/map";
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
  placement: { x: number; y: number; width: number } | null;
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
        detail: [
          v.project_id && v.mount_path,
          v.size_updated ? volumeSize(v.size_bytes) : "Size pending",
        ]
          .filter(Boolean)
          .join(" · "),
        status: owners.some((n) => tone(n.status) === "running")
          ? "running"
          : "idle",
        to: `/workspace/${id}/volumes`,
      };
    });
    return {
      projects: projectNodes,
      connections: graph.connections.map((resource) =>
        [...serviceNodes, ...volumeNodes].find(
          (node) => node.key === mapKey(resource.kind, resource.id),
        )!,
      ),
      storage: graph.storage.map((volume) =>
        volumeNodes.find((node) => node.key === mapKey("volumes", volume.id))!,
      ),
    };
  }, [graph, containers.data, releases.data, id]);
  const root = useRef<HTMLDivElement>(null);
  const [verticalLayout, setVerticalLayout] = useState(false);
  const visibleColumns = useMemo(
    () =>
      verticalLayout
        ? {
            projects: orderMapRow(columns.projects, graph.edges),
            connections: orderMapRow(columns.connections, graph.edges),
            storage: orderMapRow(columns.storage, graph.edges),
          }
        : columns,
    [verticalLayout, columns, graph.edges],
  );
  const [curves, setCurves] = useState<Curve[]>([]);
  const [hovered, setHovered] = useState<string | null>(null),
    [focused, setFocused] = useState<string | null>(null);
  const active = focused || hovered;
  const connected = active ? connectedMapNodes(active, graph.edges) : null;
  const nodes = Object.values(columns).flat();
  const connectionLabels = useMemo(() => {
    const labels = new Map<string, Set<string>>();
    for (const edge of graph.edges) {
      if (!edge.label) continue;
      for (const key of [edge.from, edge.to]) {
        if (!labels.has(key)) labels.set(key, new Set());
        labels.get(key)!.add(edge.label);
      }
    }
    return new Map(
      [...labels].map(([key, values]) => [
        key,
        ` · Connections: ${[...values].join(", ")}`,
      ]),
    );
  }, [graph.edges]);
  useLayoutEffect(() => {
    const element = root.current;
    if (!element) return;
    const measure = () => {
      const bounds = element.getBoundingClientRect();
      const vertical =
        getComputedStyle(element).getPropertyValue("--map-vertical").trim() ===
        "1";
      setVerticalLayout(vertical);
      const anchors = new Map(
        Array.from(element.querySelectorAll<HTMLElement>("[data-map-key]")).map(
          (node) => [node.dataset.mapKey!, node.getBoundingClientRect()],
        ),
      );
      const measured = graph.edges.flatMap((edge) => {
        const a = anchors.get(edge.from),
          b = anchors.get(edge.to);
        if (!a || !b) return [];
        const x1 = (vertical ? a.left + a.width / 2 : a.right) - bounds.left,
          x2 = (vertical ? b.left + b.width / 2 : b.left) - bounds.left,
          y1 = (vertical ? a.bottom : a.top + a.height / 2) - bounds.top,
          y2 = (vertical ? b.top : b.top + b.height / 2) - bounds.top;
        const bend = (vertical ? y2 - y1 : x2 - x1) * 0.5;
        const groupBounds = (key: string) =>
          Array.from(element.querySelectorAll<HTMLElement>("[data-map-key]"))
            .find((node) => node.dataset.mapKey === key)
            ?.closest(".map-column")
            ?.getBoundingClientRect();
        const sourceGroup = groupBounds(edge.from),
          targetGroup = groupBounds(edge.to);
        const [preferred, ...alternatives] = mapLabelPositions(
          { x: x1, y: y1 },
          { x: x2, y: y2 },
          vertical,
        );
        return [
          {
            ...edge,
            samples: sampleMapCurve(
              { x: x1, y: y1 },
              { x: x2, y: y2 },
              vertical,
            ),
            d: vertical
              ? `M${x1},${y1} C${x1},${y1 + bend} ${x2},${y2 - bend} ${x2},${y2}`
              : `M${x1},${y1} C${x1 + bend},${y1} ${x2 - bend},${y2} ${x2},${y2}`,
            labelBox: edge.label
              ? {
                  ...preferred,
                  alternatives,
                  width: Math.min(edge.label.length, 19) * 7 + 18,
                  left: x1,
                  right: x2,
                  top: sourceGroup ? sourceGroup.bottom - bounds.top : y1,
                  bottom: targetGroup ? targetGroup.top - bounds.top : y2,
                }
              : null,
          },
        ];
      });
      const cards = [...anchors.values()].map((rect) => ({
        x: rect.left - bounds.left,
        y: rect.top - bounds.top,
        width: rect.width,
        height: rect.height,
      }));
      const placements = placeMapLabels(
        measured.map((edge) => edge.labelBox),
        cards,
        bounds.height,
        vertical ? bounds.width : undefined,
        measured.map((edge) => edge.samples),
      );
      setCurves(
        measured.map((edge, index) => ({
          ...edge,
          placement: placements[index],
        })),
      );
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    element
      .querySelectorAll("[data-map-key]")
      .forEach((node) => observer.observe(node));
    return () => observer.disconnect();
  }, [graph, visibleColumns]);
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
                      {edge.label && edge.placement && (
                        <g
                          className="map-edge-label"
                          transform={`translate(${edge.placement.x},${edge.placement.y})`}
                        >
                          <rect
                            x={-edge.placement.width / 2}
                            y="-10"
                            width={edge.placement.width}
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
            {(["projects", "connections", "storage"] as const).map((kind) => (
              <section className="map-column" key={kind} aria-label={kind}>
                <h2>
                  {kind}
                  <span>{columns[kind].length}</span>
                </h2>
                <div
                  className="map-node-list"
                  style={
                    {
                      "--map-card-columns": Math.max(
                        1,
                        Math.min(columns[kind].length, 3),
                      ),
                    } as CSSProperties
                  }
                >
                  {visibleColumns[kind].map((node) => (
                    <Link
                      key={node.key}
                      data-map-key={node.key}
                      to={node.to}
                      className={`map-node ${connected && !connected.has(node.key) ? "is-dimmed" : ""} ${connected?.has(node.key) ? "is-connected" : ""}`}
                      onMouseEnter={() => setHovered(node.key)}
                      onMouseLeave={() => setHovered(null)}
                      onFocus={() => setFocused(node.key)}
                      onBlur={() => setFocused(null)}
                      title={`${node.name} · ${node.detail} · ${node.status}${connectionLabels.get(node.key) || ""}`}
                      aria-label={`${node.name} ${node.detail} ${node.status}${connectionLabels.get(node.key) || ""}`}
                    >
                      {node.engine ? (
                        <EngineIcon engine={node.engine} />
                      ) : (
                        <span className="map-node-icon">
                          <Icon
                            name={
                              node.key.startsWith("projects:")
                                ? "grid"
                                : "hard-drive"
                            }
                          />
                        </span>
                      )}
                      <span className="map-node-copy">
                        <strong>
                          {verticalLayout
                            ? node.name
                                .split(/(?<=-)|\s+/)
                                .map((part, index) => (
                                  <span key={index}>
                                    <span className="map-name-part">
                                      {part}
                                    </span>
                                    {part.endsWith("-") ? null : " "}
                                  </span>
                                ))
                            : node.name}
                        </strong>
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
