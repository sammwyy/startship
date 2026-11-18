import type { Project, Service, Volume } from "../services/types";

export type MapKind = "projects" | "services" | "volumes";
export interface MapEdge {
  from: string;
  to: string;
  label?: string;
  lane?: string;
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
  const serviceSlots = [
    ...orderedServices.map((s) => ({
      key: mapKey("services", s.id),
      lane: false,
      position: mean(s.attachments.map((a) => a.project_id)),
    })),
    ...orderedVolumes
      .filter((v) => positions.has(v.project_id))
      .map((v) => ({
        key: `lane:${v.id}`,
        lane: true,
        position: mean([v.project_id]),
      })),
  ]
    .sort((a, b) => a.position - b.position)
    .map(({ key, lane }) => ({ key, lane }));
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
    if (orderedVolumes.some((v) => v.id === service.volume_id))
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
        lane: `lane:${volume.id}`,
      });
  }
  return {
    projects: [...projects],
    services: orderedServices,
    volumes: orderedVolumes,
    serviceSlots,
    edges,
  };
}

export interface MapBox {
  x: number;
  y: number;
  width: number;
  height: number;
}
export function mapNodeHeight(key: string, edges: MapEdge[]) {
  const count = Math.max(
    edges.filter((edge) => edge.from === key).length,
    edges.filter((edge) => edge.to === key).length,
  );
  return Math.max(76, (count - 1) * 24 + 32);
}

export function routeMapEdges(edges: MapEdge[], boxes: Map<string, MapBox>) {
  const center = (key: string) => {
    const box = boxes.get(key);
    return box ? box.y + box.height / 2 : 0;
  };
  const port = (edge: MapEdge, source: boolean) => {
    const key = source ? edge.from : edge.to;
    const box = boxes.get(key)!;
    const peers = edges
      .filter((other) => (source ? other.from === key : other.to === key))
      .sort(
        (a, b) =>
          center(source ? a.to : a.from) - center(source ? b.to : b.from),
      );
    return {
      x: source ? box.x + box.width : box.x,
      y:
        box.y +
        box.height / 2 +
        (peers.indexOf(edge) - (peers.length - 1) / 2) * 24,
    };
  };
  const curve = (
    from: { x: number; y: number },
    to: { x: number; y: number },
  ) => {
    const bend = (to.x - from.x) / 2;
    return `C${from.x + bend},${from.y} ${to.x - bend},${to.y} ${to.x},${to.y}`;
  };
  return edges.flatMap((edge) => {
    if (!boxes.has(edge.from) || !boxes.has(edge.to)) return [];
    const lane = edge.lane ? boxes.get(edge.lane) : undefined;
    if (edge.lane && !lane) return [];
    const source = port(edge, true),
      target = port(edge, false);
    const width = edge.label ? Math.min(edge.label.length, 18) * 6 + 16 : 0;
    const lead = { x: source.x + (width ? width + 16 : 12), y: source.y };
    let d = `M${source.x},${source.y} L${lead.x},${lead.y} `;
    if (lane) {
      const entry = { x: lane.x, y: lane.y + lane.height / 2 };
      const exit = { x: lane.x + lane.width, y: entry.y };
      d += `${curve(lead, entry)} L${exit.x},${exit.y} ${curve(exit, target)}`;
    } else {
      d += curve(lead, target);
    }
    return [
      {
        ...edge,
        source,
        target,
        d,
        placement: edge.label
          ? { x: source.x + 8 + width / 2, y: source.y, width }
          : null,
      },
    ];
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
