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
import {
  buildWorkspaceMap,
  connectedMapNodes,
  mapKey,
  mapNodeHeight,
  routeMapEdges,
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
type Curve = ReturnType<typeof routeMapEdges>[number];
const isRunning = (status: string) =>
  ["running", "success", "active"].includes(status);

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
        detail: `${owned.length} container${owned.length === 1 ? "" : "s"}`,
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
        status: owners.some((n) => isRunning(n.status)) ? "running" : "idle",
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
  const [verticalLayout, setVerticalLayout] = useState(false);
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
      if (vertical) {
        setCurves([]);
        return;
      }
      const boxes = new Map(
        Array.from(element.querySelectorAll<HTMLElement>("[data-map-key]")).map(
          (node) => {
            const rect = node.getBoundingClientRect();
            return [
              node.dataset.mapKey!,
              {
                x: rect.left - bounds.left,
                y: rect.top - bounds.top,
                width: rect.width,
                height: rect.height,
              },
            ] as const;
          },
        ),
      );
      setCurves(routeMapEdges(graph.edges, boxes));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    element
      .querySelectorAll("[data-map-key]")
      .forEach((node) => observer.observe(node));
    return () => observer.disconnect();
  }, [graph, columns, verticalLayout]);
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
  const nodeByKey = new Map(nodes.map((node) => [node.key, node]));
  const card = (node: Node, alias?: string) => (
    <Link
      key={node.key}
      data-map-key={verticalLayout ? undefined : node.key}
      to={node.to}
      style={
        verticalLayout
          ? undefined
          : { minHeight: mapNodeHeight(node.key, graph.edges) }
      }
      className={`map-node ${!verticalLayout && connected && !connected.has(node.key) ? "is-dimmed" : ""} ${!verticalLayout && connected?.has(node.key) ? "is-connected" : ""}`}
      onMouseEnter={() => setHovered(node.key)}
      onMouseLeave={() => setHovered(null)}
      onFocus={() => setFocused(node.key)}
      onBlur={() => setFocused(null)}
      title={`${alias ? alias + " → " : ""}${node.name} · ${node.detail} · ${node.status}${connectionLabels.get(node.key) || ""}`}
      aria-label={`${alias ? alias + " → " : ""}${node.name} ${node.detail} ${node.status}${connectionLabels.get(node.key) || ""}`}
    >
      {node.engine ? (
        <EngineIcon engine={node.engine} />
      ) : (
        <span className="map-node-icon">
          <Icon
            name={node.key.startsWith("projects:") ? "grid" : "hard-drive"}
          />
        </span>
      )}
      <span className="map-node-copy">
        <strong>
          {alias && <span className="map-tree-alias">{alias} → </span>}
          {node.name}
        </strong>
        <small>
          <span>{node.detail}</span>
          <span>· {node.status}</span>
        </small>
      </span>
    </Link>
  );
  const children = (key: string) =>
    graph.edges.filter((edge) => edge.from === key);
  const branch = (key: string, alias?: string) => {
    const node = nodeByKey.get(key);
    if (!node) return null;
    return (
      <li key={key}>
        {card(node, alias)}
        {children(key).length > 0 && (
          <ul>
            {children(key).map((edge) => {
              const child = nodeByKey.get(edge.to);
              return child ? (
                <li key={edge.to}>{card(child, edge.label)}</li>
              ) : null;
            })}
          </ul>
        )}
      </li>
    );
  };
  const shown = new Set<string>();
  for (const project of columns.projects) {
    for (const edge of children(project.key)) {
      shown.add(edge.to);
      for (const child of children(edge.to)) shown.add(child.to);
    }
  }
  const orphanServices = columns.services.filter(
    (node) => !shown.has(node.key),
  );
  for (const service of orphanServices) {
    for (const edge of children(service.key)) shown.add(edge.to);
  }
  const orphanVolumes = columns.volumes.filter((node) => !shown.has(node.key));
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
            {liveStatus !== "connected" && (
              <span>
                {liveStatus === "disconnected"
                  ? "Updates disconnected"
                  : "Connecting…"}
              </span>
            )}
          </div>
          <div className="map-board" ref={root}>
            {verticalLayout ? (
              <div className="map-tree">
                {columns.projects.map((project) => (
                  <section
                    key={project.key}
                    aria-label={project.name}
                    className="map-tree-project"
                  >
                    {card(project)}
                    {children(project.key).length ? (
                      <ul>
                        {children(project.key).map((edge) =>
                          branch(edge.to, edge.label),
                        )}
                      </ul>
                    ) : (
                      <p className="map-tree-empty">No connections yet</p>
                    )}
                  </section>
                ))}
                {(orphanServices.length > 0 || orphanVolumes.length > 0) && (
                  <section
                    aria-label="Not connected"
                    className="map-tree-orphans"
                  >
                    <h2>
                      Not connected{" "}
                      <span>
                        {orphanServices.length + orphanVolumes.length}
                      </span>
                    </h2>
                    <ul>
                      {orphanServices.map((node) => branch(node.key))}
                      {orphanVolumes.map((node) => branch(node.key))}
                    </ul>
                  </section>
                )}
              </div>
            ) : (
              <>
                <svg className="map-edges" aria-hidden="true">
                  {curves.map((edge, i) => {
                    const live = [edge.from, edge.to].every((key) =>
                      isRunning(nodeByKey.get(key)?.status || ""),
                    );
                    const dim = connected && !connected.has(edge.from);
                    return (
                      <g
                        key={i}
                        className={`${live ? "is-live" : ""} ${dim ? "is-dimmed" : ""}`}
                      >
                        <path d={edge.d} />
                        <circle cx={edge.source.x} cy={edge.source.y} r="2.5" />
                        <circle cx={edge.target.x} cy={edge.target.y} r="2.5" />
                        {edge.label && edge.placement && (
                          <g
                            className="map-edge-label"
                            transform={`translate(${edge.placement.x},${edge.placement.y})`}
                          >
                            <title>{edge.label}</title>
                            <rect
                              x={-edge.placement.width / 2}
                              y="-9"
                              width={edge.placement.width}
                              height="18"
                              rx="9"
                            />
                            <text
                              textAnchor="middle"
                              dominantBaseline="central"
                            >
                              {edge.label.length > 18
                                ? `${edge.label.slice(0, 17)}…`
                                : edge.label}
                            </text>
                          </g>
                        )}
                      </g>
                    );
                  })}
                </svg>
                {(["projects", "services", "volumes"] as const).map((kind) => (
                  <section className="map-column" key={kind} aria-label={kind}>
                    <h2>
                      {kind}
                      <span>{columns[kind].length}</span>
                    </h2>
                    <div className="map-node-list">
                      {kind === "services"
                        ? graph.serviceSlots.map((slot) =>
                            slot.lane ? (
                              <div
                                key={slot.key}
                                data-map-key={slot.key}
                                className="map-lane"
                                aria-hidden="true"
                              />
                            ) : (
                              card(nodeByKey.get(slot.key)!)
                            ),
                          )
                        : columns[kind].map((node) => card(node))}
                      {!columns[kind].length && (
                        <p className="map-column-empty">No {kind} yet</p>
                      )}
                    </div>
                  </section>
                ))}
              </>
            )}
          </div>
          <p className="map-hint">Hover or focus to trace connections</p>
        </>
      )}
    </div>
  );
}
