import { describe, expect, it } from "vitest";
import {
  buildWorkspaceMap,
  connectedMapNodes,
  placeMapLabels,
  orderMapRow,
  mapLabelPositions,
} from "./map";
import type { Project, Service, Volume } from "../services/types";

const projects = [{ id: "a" }, { id: "b" }, { id: "c" }] as Project[];
const service = (id: string, ids: string[], volume_id = "") =>
  ({
    id,
    volume_id,
    attachments: ids.map((project_id) => ({ project_id, alias: "db" })),
  }) as Service;
const volume = (id: string, project_id = "") =>
  ({ id, project_id, mount_path: "/data" }) as Volume;
describe("workspace map", () => {
  it("handles an empty workspace", () => {
    expect(buildWorkspaceMap([], [], [])).toEqual({
      projects: [],
      services: [],
      volumes: [],
      connections: [],
      storage: [],
      edges: [],
    });
  });
  it("orders by connected project barycenters with unconnected resources last", () => {
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
    expect(graph.edges).toContainEqual({
      from: "projects:a",
      to: "services:first",
      label: "db",
    });
    expect(graph.edges).toContainEqual({
      from: "projects:a",
      to: "volumes:first-v",
      label: "/data",
    });
    expect(graph.edges).toContainEqual({
      from: "services:middle",
      to: "volumes:mid-v",
    });
    expect(graph.connections).toEqual([
      { kind: "services", id: "first" },
      { kind: "volumes", id: "first-v" },
      { kind: "services", id: "middle" },
      { kind: "services", id: "late" },
      { kind: "services", id: "unused" },
    ]);
    expect(graph.storage.map((v) => v.id)).toEqual([
      "mid-v",
      "late-v",
      "unused-v",
    ]);
    const column = new Map<string, number>([
      ...graph.projects.map((p) => [`projects:${p.id}`, 0] as const),
      ...graph.connections.map((n) => [`${n.kind}:${n.id}`, 1] as const),
      ...graph.storage.map((v) => [`volumes:${v.id}`, 2] as const),
    ]);
    for (const edge of graph.edges)
      expect(column.get(edge.to)! - column.get(edge.from)!).toBe(1);
    expect(services[0].id).toBe("unused");
  });
  it("keeps project mounts in connections even with a dangling project reference", () => {
    const graph = buildWorkspaceMap(
      projects,
      [service("s", ["a"], "v")],
      [volume("v", "missing")],
    );
    expect(graph.connections).toContainEqual({ kind: "volumes", id: "v" });
    expect(graph.storage).toEqual([]);
    expect(graph.edges).not.toContainEqual({
      from: "services:s",
      to: "volumes:v",
    });
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

describe("map label placement", () => {
  const label = { x: 175, y: 80, width: 74, left: 100, right: 240 };
  it("keeps labels near their target and separates colliding boxes", () => {
    const placed = placeMapLabels([label, label], [], 200);
    expect(placed[0]).toEqual({ x: 175, y: 80, width: 74 });
    expect(placed[1]).toEqual({ x: 175, y: 56, width: 74 });
  });
  it("samples the preferred and fallback points on horizontal and vertical curves", () => {
    const horizontal = mapLabelPositions(
      { x: 0, y: 0 },
      { x: 200, y: 100 },
      false,
    );
    expect(horizontal[0]).toEqual({ x: 140.625, y: 84.375 });
    expect(horizontal[1]).toEqual({ x: 100, y: 50 });
    expect(horizontal[2]).toEqual({ x: 59.375, y: 15.625 });
    expect(horizontal).toHaveLength(4);
    expect(mapLabelPositions({ x: 0, y: 0 }, { x: 100, y: 200 }, true)).toEqual(
      horizontal.map(({ x, y }) => ({ x: y, y: x })),
    );
  });
  it("retries alternate curve points after the preferred desktop region is exhausted", () => {
    const retry = { ...label, y: 120, alternatives: [{ x: 175, y: 360 }] };
    const cards = [{ x: 100, y: 0, width: 140, height: 240 }];
    expect(placeMapLabels([retry], cards, 500)).toEqual([
      { x: 175, y: 360, width: 74 },
    ]);
    expect(
      placeMapLabels([retry], [{ ...cards[0], height: 500 }], 500),
    ).toEqual([null]);
  });
  it("retries alternate points within the mobile row gap after horizontal nudges fail", () => {
    const retry = {
      ...label,
      x: 150,
      y: 240,
      top: 200,
      bottom: 400,
      alternatives: [{ x: 150, y: 320 }],
    };
    expect(
      placeMapLabels(
        [retry],
        [{ x: 0, y: 220, width: 390, height: 40 }],
        500,
        390,
      ),
    ).toEqual([{ x: 150, y: 320, width: 74 }]);
  });
  it("clamps labels to the gutter and avoids cards", () => {
    const [placed] = placeMapLabels(
      [{ ...label, x: 235 }],
      [{ x: 140, y: 70, width: 80, height: 20 }],
      200,
    );
    expect(placed).toEqual({ x: 197, y: 56, width: 74 });
  });
  it("hides labels with insufficient space or no collision-free position", () => {
    expect(
      placeMapLabels(
        [null, { ...label, right: 150 }, label],
        [{ x: 100, y: 0, width: 140, height: 200 }],
        200,
      ),
    ).toEqual([null, null, null]);
    expect(placeMapLabels([label], [], 10)).toEqual([null]);
    expect(placeMapLabels([], [], 200)).toEqual([]);
  });
  it("separates vertical-layout labels along x within the row gap", () => {
    const vertical = { ...label, x: 150, y: 240, top: 200, bottom: 264 };
    expect(placeMapLabels([vertical, vertical], [], 500, 390)).toEqual([
      { x: 150, y: 240, width: 74 },
      { x: 70, y: 240, width: 74 },
    ]);
  });
  it("keeps vertical labels in the board and hides labels in blocked or undersized gaps", () => {
    const vertical = { ...label, x: 380, y: 260, top: 200, bottom: 264 };
    expect(placeMapLabels([vertical], [], 500, 390)).toEqual([
      { x: 347, y: 248, width: 74 },
    ]);
    expect(
      placeMapLabels(
        [vertical],
        [{ x: 0, y: 200, width: 390, height: 64 }],
        500,
        390,
      ),
    ).toEqual([null]);
    expect(
      placeMapLabels([{ ...vertical, bottom: 220 }], [], 500, 390),
    ).toEqual([null]);
    expect(placeMapLabels([vertical], [], 500, 60)).toEqual([null]);
  });
});

describe("wrapping map rows", () => {
  const nodes = ["out", "both", "idle", "in"].map((key) => ({ key }));
  const edges = [
    { from: "previous", to: "in" },
    { from: "previous", to: "both" },
    { from: "both", to: "next" },
    { from: "out", to: "next" },
  ];
  it("keeps incoming nodes first and outgoing nodes last when wrapping", () => {
    expect(orderMapRow(nodes, edges).map((node) => node.key)).toEqual([
      "in",
      "both",
      "idle",
      "out",
    ]);
    expect(nodes.map((node) => node.key)).toEqual([
      "out",
      "both",
      "idle",
      "in",
    ]);
  });
  it("preserves a single line, empty rows and rows without connections", () => {
    expect(orderMapRow(nodes.slice(0, 3), edges)).toEqual(nodes.slice(0, 3));
    expect(orderMapRow([], edges)).toEqual([]);
    expect(orderMapRow(nodes, [])).toEqual(nodes);
    expect(
      orderMapRow(nodes, [{ from: "missing", to: "also-missing" }]),
    ).toEqual(nodes);
  });
});
