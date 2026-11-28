import { describe, expect, it } from "vitest";
import {
  buildWorkspaceMap as build,
  connectedMapNodes,
  mapNodeHeight,
  routeMapEdges,
} from "./map";
import type { MapBox, MapEdge } from "./map";
import type { Connection, Project, Service, Volume } from "../services/types";

const projects = [{ id: "a" }, { id: "b" }, { id: "c" }] as Project[];
const links = new Map<string, Connection[]>();
const service = (id: string, ids: string[], volume_id = "") => {
  links.set(
    id,
    ids.map(
      (project_id) =>
        ({
          id: project_id + id,
          from: { kind: "project", id: project_id },
          to: { kind: "service", id },
          alias: "db",
        }) as Connection,
    ),
  );
  return { id, volume_id } as Service;
};
const buildWorkspaceMap = (p: Project[], s: Service[], v: Volume[]) =>
  build(
    p,
    s,
    v,
    s.flatMap((x) => links.get(x.id) || []),
  );
const volume = (id: string, project_id = "") =>
  ({ id, project_id, mount_path: "/data" }) as Volume;

describe("workspace map", () => {
  it("handles an empty workspace", () => {
    expect(buildWorkspaceMap([], [], [])).toEqual({
      projects: [],
      services: [],
      volumes: [],
      serviceSlots: [],
      edges: [],
    });
  });
  it("orders resources and invisible mount lanes by project barycenters, leaving orphans last", () => {
    const services = [
      service("unused", []),
      service("late", ["c"], "late-v"),
      service("middle", ["a", "c"], "mid-v"),
      service("first", ["a"]),
    ];
    const graph = buildWorkspaceMap(projects, services, [
      volume("unused-v"),
      volume("late-v"),
      volume("mid-v"),
      volume("first-v", "a"),
    ]);
    expect(graph.services.map((s) => s.id)).toEqual([
      "first",
      "middle",
      "late",
      "unused",
    ]);
    expect(graph.volumes.map((v) => v.id)).toEqual([
      "first-v",
      "mid-v",
      "late-v",
      "unused-v",
    ]);
    expect(graph.serviceSlots).toEqual([
      { key: "services:first", lane: false },
      { key: "lane:first-v", lane: true },
      { key: "services:middle", lane: false },
      { key: "services:late", lane: false },
      { key: "services:unused", lane: false },
    ]);
    expect(graph.edges).toContainEqual({
      from: "projects:a",
      to: "services:first",
      label: "db",
    });
    expect(graph.edges).toContainEqual({
      from: "projects:a",
      to: "volumes:first-v",
      label: "/data",
      lane: "lane:first-v",
    });
    expect(graph.edges).toContainEqual({
      from: "services:middle",
      to: "volumes:mid-v",
    });
    expect(services[0].id).toBe("unused");
  });
  it("keeps all volumes in the volume column and creates no lane for dangling mounts", () => {
    const graph = buildWorkspaceMap(
      projects,
      [service("s", ["a"], "v")],
      [volume("v", "missing")],
    );
    expect(graph.serviceSlots).toEqual([{ key: "services:s", lane: false }]);
    expect(graph.volumes.map((v) => v.id)).toEqual(["v"]);
    expect(graph.edges).toContainEqual({ from: "services:s", to: "volumes:v" });
    expect(graph.edges.some((edge) => edge.lane)).toBe(false);
  });
  it("ignores dangling references and keeps ties stable", () => {
    const graph = buildWorkspaceMap(
      projects,
      [service("x", ["missing"], "missing"), service("y", [])],
      [volume("v", "missing")],
    );
    expect(graph.edges).toEqual([]);
    expect(graph.services.map((s) => s.id)).toEqual(["x", "y"]);
  });
  it("traverses shared connections in both directions without cycling", () => {
    const edges = [
      { from: "p", to: "s" },
      { from: "q", to: "s" },
      { from: "s", to: "v" },
    ];
    expect([...connectedMapNodes("v", edges)].sort()).toEqual([
      "p",
      "q",
      "s",
      "v",
    ]);
    expect([...connectedMapNodes("alone", edges)]).toEqual(["alone"]);
  });
});

describe("map ports and lanes", () => {
  const box = (x: number, y: number, height = 80): MapBox => ({
    x,
    y,
    width: 160,
    height,
  });
  const boxes = new Map([
    ["p", box(0, 0)],
    ["q", box(0, 140)],
    ["s", box(340, 100)],
    ["t", box(340, 240)],
    ["v", box(680, 0)],
    ["lane", box(340, 0, 20)],
  ]);
  it("spreads outgoing ports by target y and incoming ports by source y, independently of edge order", () => {
    const edges = [
      { from: "p", to: "t", label: "cache" },
      { from: "q", to: "s", label: "jobs" },
      { from: "p", to: "s", label: "db" },
    ];
    const paths = routeMapEdges(edges, boxes);
    expect(paths[0].source).toEqual({ x: 160, y: 52 });
    expect(paths[2].source).toEqual({ x: 160, y: 28 });
    expect(paths[1].target).toEqual({ x: 340, y: 152 });
    expect(paths[2].target).toEqual({ x: 340, y: 128 });
    for (const path of paths) {
      expect(path.placement!.y).toBe(path.source.y);
      expect(path.placement!.x - path.placement!.width / 2).toBe(
        path.source.x + 8,
      );
      expect(path.d).toMatch(/^M[\d,]+ L[\d,]+ C/);
    }
  });
  it("keeps mounted-volume paths horizontal across their invisible service slot", () => {
    const [path] = routeMapEdges(
      [{ from: "p", to: "v", label: "/app/uploads", lane: "lane" }],
      boxes,
    );
    expect(path.d).toContain("340,10 L500,10 C");
    expect(path.d).toMatch(/680,40$/);
    expect(path.lane).toBe("lane");
    expect(path.placement).not.toBeNull();
  });
  it("keeps long labels visible and bounded on the source lead, without clipping into cards", () => {
    const [path] = routeMapEdges(
      [{ from: "p", to: "s", label: "/a/very/long/mounted/volume/path" }],
      boxes,
    );
    expect(path.label).toBe("/a/very/long/mounted/volume/path");
    expect(path.placement!.width).toBe(124);
    expect(path.placement!.x + path.placement!.width / 2).toBeLessThan(340);
    expect(
      routeMapEdges([{ from: "p", to: "s" }], boxes)[0].placement,
    ).toBeNull();
  });
  it("ignores missing anchors and missing lanes, and supports an empty graph", () => {
    expect(routeMapEdges([], boxes)).toEqual([]);
    expect(
      routeMapEdges(
        [
          { from: "missing", to: "s" },
          { from: "p", to: "s", lane: "missing" },
        ],
        boxes,
      ),
    ).toEqual([]);
  });
  it("grows cards to contain all ports and leaves breathing room around them", () => {
    const edges: MapEdge[] = Array.from({ length: 8 }, (_, i) => ({
      from: "p",
      to: "s" + i,
    }));
    expect(mapNodeHeight("p", edges)).toBe(200);
    expect(mapNodeHeight("unused", edges)).toBe(76);
    expect(mapNodeHeight("s0", edges)).toBe(76);
    const paths = routeMapEdges(
      edges,
      new Map([
        ["p", box(0, 0, mapNodeHeight("p", edges))],
        ...edges.map((edge, i) => [edge.to, box(340, i * 100)] as const),
      ]),
    );
    expect(paths[0].source.y).toBe(16);
    expect(paths.at(-1)!.source.y).toBe(184);
  });
});
