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
    if (volumes.some((v) => v.id === service.volume_id))
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
    edges,
  };
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
