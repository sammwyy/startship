import { describe, expect, it } from "vitest";
import { buildWorkspaceMap, connectedMapNodes } from "./map";
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
    expect(services[0].id).toBe("unused");
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
