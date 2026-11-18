import type { Project, Service, Volume } from "../services/types";

export type MapKind = "projects" | "services" | "volumes";
export interface MapEdge {
  from: string;
  to: string;
  label?: string;
}
export const mapKey = (kind: MapKind, id: string) => `${kind}:${id}`;

export function buildWorkspaceMap(
  projects: Project[],
  services: Service[],
  volumes: Volume[],
) {
  const positions = new Map(projects.map((p, i) => [p.id, i]));
  const mean = (ids: string[]) => {
    const values = [...new Set(ids)].flatMap((id) =>
      positions.has(id) ? [positions.get(id)!] : [],
    );
    return values.length
      ? values.reduce((a, b) => a + b, 0) / values.length
      : Infinity;
  };
  const orderedServices = [...services].sort(
    (a, b) =>
      mean(a.attachments.map((x) => x.project_id)) -
      mean(b.attachments.map((x) => x.project_id)),
  );
  const orderedVolumes = [...volumes].sort(
    (a, b) =>
      mean([
        a.project_id,
        ...services
          .filter((s) => s.volume_id === a.id)
          .flatMap((s) => s.attachments.map((x) => x.project_id)),
      ]) -
      mean([
        b.project_id,
        ...services
          .filter((s) => s.volume_id === b.id)
          .flatMap((s) => s.attachments.map((x) => x.project_id)),
      ]),
  );
  const connections = [
    ...orderedServices.map((s) => ({
      kind: "services" as const,
      id: s.id,
      position: mean(s.attachments.map((a) => a.project_id)),
    })),
    ...orderedVolumes
      .filter((v) => v.project_id)
      .map((v) => ({
        kind: "volumes" as const,
        id: v.id,
        position: mean([v.project_id]),
      })),
  ]
    .sort((a, b) => a.position - b.position)
    .map(({ kind, id }) => ({ kind, id }));
  const storage = orderedVolumes.filter((v) => !v.project_id);
  const edges: MapEdge[] = [];
  for (const service of orderedServices) {
    for (const attachment of service.attachments) {
      if (positions.has(attachment.project_id))
        edges.push({
          from: mapKey("projects", attachment.project_id),
          to: mapKey("services", service.id),
          label: attachment.alias,
        });
    }
    if (storage.some((v) => v.id === service.volume_id))
      edges.push({
        from: mapKey("services", service.id),
        to: mapKey("volumes", service.volume_id),
      });
  }
  for (const volume of orderedVolumes) {
    if (positions.has(volume.project_id))
      edges.push({
        from: mapKey("projects", volume.project_id),
        to: mapKey("volumes", volume.id),
        label: volume.mount_path,
      });
  }
  return {
    projects: [...projects],
    services: orderedServices,
    volumes: orderedVolumes,
    connections,
    storage,
    edges,
  };
}

export interface MapBox {
  x: number;
  y: number;
  width: number;
  height: number;
}
export interface MapLabel {
  x: number;
  y: number;
  width: number;
  left: number;
  right: number;
}
export function placeMapLabels(
  labels: (MapLabel | null)[],
  cards: MapBox[],
  height: number,
) {
  const placed: MapBox[] = [];
  const overlaps = (a: MapBox, b: MapBox) =>
    a.x < b.x + b.width + 4 &&
    a.x + a.width + 4 > b.x &&
    a.y < b.y + b.height + 4 &&
    a.y + a.height + 4 > b.y;
  return labels.map((label) => {
    if (!label || label.right - label.left < label.width + 12) return null;
    const x = Math.max(
      label.left + 6 + label.width / 2,
      Math.min(label.x, label.right - 6 - label.width / 2),
    );
    for (const offset of [0, -24, 24, -48, 48, -72, 72, -96, 96]) {
      const y = label.y + offset;
      const box = {
        x: x - label.width / 2,
        y: y - 10,
        width: label.width,
        height: 20,
      };
      if (
        box.y < 0 ||
        box.y + box.height > height ||
        [...cards, ...placed].some((other) => overlaps(box, other))
      )
        continue;
      placed.push(box);
      return { x, y, width: label.width };
    }
    return null;
  });
}

export function connectedMapNodes(start: string, edges: MapEdge[]) {
  const connected = new Set([start]);
  const queue = [start];
  for (const key of queue) {
    for (const edge of edges) {
      const next =
        edge.from === key ? edge.to : edge.to === key ? edge.from : undefined;
      if (next && !connected.has(next)) {
        connected.add(next);
        queue.push(next);
      }
    }
  }
  return connected;
}
