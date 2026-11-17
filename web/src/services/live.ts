import type { QueryClient } from "@tanstack/react-query";
import { queries } from "./queries";
import type {
  Container,
  DomainRoute,
  Project,
  Release,
  SystemVersion,
  UpdateInfo,
} from "./types";

export type LiveSubscription =
  | { topic: "project"; project_id: string }
  | { topic: "workspace"; workspace_id: string }
  | { topic: "release_log"; release_id: string }
  | { topic: "container_log"; container_id: string }
  | { topic: "traffic"; project_id: string; window: number }
  | { topic: "system_update" };
export interface LiveMessage {
  type: "snapshot" | "upsert" | "delete" | "log" | "error";
  id: string;
  topic: LiveSubscription["topic"];
  data?: unknown;
  kind?: "project" | "container" | "release" | "route";
  record?: Project | Container | Release | DomainRoute;
  record_id?: string;
  reset?: boolean;
  error?: string;
}
export type LiveStatus =
  "connecting" | "connected" | "reconnecting" | "disconnected";
type Listener = (message: LiveMessage) => void;

export class LiveConnection {
  private socket?: WebSocket;
  private timer?: ReturnType<typeof setTimeout>;
  private stopped = true;
  private ready = false;
  private attempt = 0;
  private nextId = 0;
  private subscriptions = new Map<
    string,
    { id: string; spec: LiveSubscription; listeners: Set<Listener> }
  >();
  constructor(
    private onMessage: (spec: LiveSubscription, message: LiveMessage) => void,
    private onStatus: (status: LiveStatus) => void,
    private onReady: (version: SystemVersion) => void,
  ) {}
  connect() {
    this.stopped = false;
    if (this.socket || this.timer) return;
    this.onStatus(this.attempt ? "reconnecting" : "connecting");
    const url = new URL("/api/live", window.location.href);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(url);
    this.socket = socket;
    socket.onmessage = (event) => {
      if (this.socket !== socket) return;
      let message;
      try {
        message = JSON.parse(String(event.data));
      } catch {
        return;
      }
      if (!message || typeof message !== "object") return;
      if (message.type === "ready") {
        this.ready = true;
        this.attempt = 0;
        this.onStatus("connected");
        if (
          typeof message.version === "string" &&
          typeof message.commit === "string"
        )
          this.onReady({ version: message.version, commit: message.commit });
        for (const sub of this.subscriptions.values())
          this.send({ type: "subscribe", id: sub.id, ...sub.spec });
        return;
      }
      const sub = [...this.subscriptions.values()].find(
        (s) => s.id === message.id && s.spec.topic === message.topic,
      );
      if (
        !sub ||
        !["snapshot", "upsert", "delete", "log", "error"].includes(message.type)
      )
        return;
      this.onMessage(sub.spec, message);
      sub.listeners.forEach((listener) => listener(message));
    };
    socket.onclose = (event) => {
      if (this.socket !== socket) return;
      this.socket = undefined;
      this.ready = false;
      if (this.stopped) return;
      if (event.code === 4401) {
        this.disconnect();
        window.dispatchEvent(new Event("session-expired"));
        return;
      }
      this.onStatus("reconnecting");
      const delay = Math.min(10000, 500 * 2 ** Math.min(this.attempt++, 5));
      this.timer = setTimeout(() => {
        this.timer = undefined;
        this.connect();
      }, delay);
    };
  }
  disconnect() {
    this.stopped = true;
    this.ready = false;
    this.attempt = 0;
    clearTimeout(this.timer);
    this.timer = undefined;
    const socket = this.socket;
    this.socket = undefined;
    socket?.close();
    this.onStatus("disconnected");
  }
  subscribe(spec: LiveSubscription, listener: Listener = () => {}) {
    const key = JSON.stringify(spec);
    let sub = this.subscriptions.get(key);
    if (!sub) {
      sub = { id: `sub-${++this.nextId}`, spec, listeners: new Set() };
      this.subscriptions.set(key, sub);
      this.send({ type: "subscribe", id: sub.id, ...spec });
    }
    sub.listeners.add(listener);
    return () => {
      sub.listeners.delete(listener);
      if (!sub.listeners.size) {
        this.subscriptions.delete(key);
        this.send({ type: "unsubscribe", id: sub.id });
      }
    };
  }
  private send(message: object) {
    if (this.ready && this.socket?.readyState === WebSocket.OPEN)
      this.socket.send(JSON.stringify(message));
  }
}

function replaceRecord<T extends { id: string }>(
  list: T[] | undefined,
  message: LiveMessage,
): T[] | undefined {
  if (!list) return list;
  if (message.type === "delete")
    return list.filter((record) => record.id !== message.record_id);
  if (!message.record) return list;
  const record = message.record as unknown as T;
  return list.some((r) => r.id === record.id)
    ? list.map((r) => (r.id === record.id ? { ...r, ...record } : r))
    : [...list, record];
}
export function applyLiveMessage(
  client: QueryClient,
  spec: LiveSubscription,
  event: LiveMessage,
) {
  if (event.type === "error") return;
  if (spec.topic === "project") {
    const key = {
      project: queries.project(spec.project_id).queryKey,
      container: queries.containers(spec.project_id).queryKey,
      release: queries.releases(spec.project_id).queryKey,
      route: queries.routes(spec.project_id).queryKey,
    };
    for (const queryKey of [
      ...Object.values(key),
      queries.containers().queryKey,
      queries.releases().queryKey,
    ])
      void client.cancelQueries({ queryKey, exact: true });
    if (event.type === "snapshot") {
      const data = event.data as {
        project: Project;
        containers: Container[];
        releases: Release[];
        routes: DomainRoute[];
      };
      client.setQueryData(key.project, data.project);
      client.setQueryData(key.container, data.containers);
      client.setQueryData(key.release, data.releases);
      client.setQueryData(key.route, data.routes);
      for (const [query, records] of [
        [queries.containers().queryKey, data.containers],
        [queries.releases().queryKey, data.releases],
      ] as const) {
        client.setQueryData<(Container | Release)[]>(query, (old) =>
          old
            ? [
                ...old.filter((r) => r.project_id !== spec.project_id),
                ...records,
              ]
            : undefined,
        );
      }
    } else if (event.kind === "project") {
      if (event.type === "delete")
        void client.resetQueries({ queryKey: key.project, exact: true });
      else client.setQueryData(key.project, event.record as Project);
    } else if (event.kind) {
      client.setQueryData<(Container | Release | DomainRoute)[]>(
        key[event.kind],
        (old) => replaceRecord(old, event),
      );
      if (event.kind === "container")
        client.setQueryData<Container[]>(queries.containers().queryKey, (old) =>
          replaceRecord(old, event),
        );
      if (event.kind === "release")
        client.setQueryData<Release[]>(queries.releases().queryKey, (old) =>
          replaceRecord(old, event),
        );
    }
  } else if (spec.topic === "workspace") {
    const key = queries.projects(spec.workspace_id).queryKey;
    void client.cancelQueries({ queryKey: key, exact: true });
    if (event.type === "snapshot")
      client.setQueryData(
        key,
        (event.data as { projects: Project[] }).projects,
      );
    else
      client.setQueryData<Project[]>(key, (old) => replaceRecord(old, event));
  } else if (spec.topic === "release_log" || spec.topic === "container_log") {
    const key = [
      spec.topic === "release_log"
        ? `releases/${spec.release_id}`
        : `containers/${spec.container_id}/logs`,
    ];
    void client.cancelQueries({ queryKey: key, exact: true });
    if (event.type === "snapshot") client.setQueryData(key, event.data);
    else if (event.type === "log")
      client.setQueryData<Partial<Release>>(key, (old) => ({
        ...old,
        log: event.reset
          ? String(event.data ?? "")
          : (old?.log || "") + String(event.data ?? ""),
      }));
    else if (event.record)
      client.setQueryData(key, (old: Partial<Release> | undefined) => ({
        ...old,
        ...event.record,
      }));
  } else if (spec.topic === "traffic" && event.type === "snapshot") {
    void client.cancelQueries({
      queryKey: ["traffic", spec.project_id, spec.window],
      exact: true,
    });
    client.setQueryData(["traffic", spec.project_id, spec.window], event.data);
  } else if (spec.topic === "system_update" && event.type === "snapshot") {
    void client.cancelQueries({
      queryKey: queries.updates().queryKey,
      exact: true,
    });
    client.setQueryData<UpdateInfo>(
      queries.updates().queryKey,
      event.data as UpdateInfo,
    );
  }
}
