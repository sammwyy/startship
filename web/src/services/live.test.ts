import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { LiveConnection, applyLiveMessage, type LiveMessage } from "./live";
import { queries } from "./queries";

class Socket {
  static OPEN = 1;
  static instances: Socket[] = [];
  readyState = 1;
  send = vi.fn();
  close = vi.fn();
  onmessage?: (event: MessageEvent) => void;
  onclose?: (event: CloseEvent) => void;
  constructor(public url: URL) {
    Socket.instances.push(this);
  }
  receive(message: object) {
    this.onmessage?.({ data: JSON.stringify(message) } as MessageEvent);
  }
  disconnect(code = 1006) {
    this.onclose?.({ code } as CloseEvent);
  }
}
beforeEach(() => {
  Socket.instances = [];
  vi.stubGlobal("WebSocket", Socket);
  vi.useFakeTimers();
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});
describe("authenticated live connection", () => {
  it("uses a single same-origin cookie socket and deduplicates topic subscriptions", () => {
    const ready = vi.fn();
    const connection = new LiveConnection(vi.fn(), vi.fn(), ready);
    const spec = { topic: "project" as const, project_id: "p1" };
    const a = connection.subscribe(spec);
    const b = connection.subscribe(spec);
    connection.connect();
    connection.connect();
    expect(Socket.instances).toHaveLength(1);
    const socket = Socket.instances[0];
    expect(String(socket.url)).toBe(
      `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/live`,
    );
    expect(socket.send).not.toHaveBeenCalled();
    socket.receive({ type: "ready", version: "v1", commit: "abc" });
    expect(ready).toHaveBeenCalledWith({ version: "v1", commit: "abc" });
    expect(socket.send).toHaveBeenCalledTimes(1);
    a();
    expect(socket.send).toHaveBeenCalledTimes(1);
    b();
    expect(JSON.parse(socket.send.mock.calls[1][0])).toEqual({
      type: "unsubscribe",
      id: "sub-1",
    });
    connection.disconnect();
  });
  it("reconnects with backoff, re-subscribes, and ignores stale socket messages", () => {
    const receive = vi.fn();
    const connection = new LiveConnection(receive, vi.fn(), vi.fn());
    connection.subscribe({ topic: "system_update" });
    connection.connect();
    const first = Socket.instances[0];
    first.receive({ type: "ready" });
    first.disconnect();
    vi.advanceTimersByTime(499);
    expect(Socket.instances).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(Socket.instances).toHaveLength(2);
    first.receive({
      id: "sub-1",
      topic: "system_update",
      type: "snapshot",
      data: {},
    });
    expect(receive).not.toHaveBeenCalled();
    const second = Socket.instances[1];
    second.receive({ type: "ready" });
    expect(JSON.parse(second.send.mock.calls[0][0])).toEqual({
      type: "subscribe",
      id: "sub-1",
      topic: "system_update",
    });
    second.receive({
      id: "sub-1",
      topic: "system_update",
      type: "snapshot",
      data: { current: "v2" },
    });
    expect(receive).toHaveBeenCalledOnce();
    connection.disconnect();
    vi.advanceTimersByTime(60000);
    expect(Socket.instances).toHaveLength(2);
  });
  it("expires the session without retrying a revoked connection", () => {
    const expired = vi.fn();
    window.addEventListener("session-expired", expired);
    const connection = new LiveConnection(vi.fn(), vi.fn(), vi.fn());
    connection.connect();
    Socket.instances[0].disconnect(4401);
    vi.advanceTimersByTime(60000);
    expect(expired).toHaveBeenCalledOnce();
    expect(Socket.instances).toHaveLength(1);
    window.removeEventListener("session-expired", expired);
  });
  it("keeps subscriptions across a credential rotation pause", () => {
    const connection = new LiveConnection(vi.fn(), vi.fn(), vi.fn());
    connection.subscribe({ topic: "release_log", release_id: "r1" });
    connection.connect();
    connection.disconnect();
    connection.connect();
    Socket.instances[1].receive({ type: "ready" });
    expect(JSON.parse(Socket.instances[1].send.mock.calls[0][0])).toMatchObject(
      { topic: "release_log", release_id: "r1" },
    );
    connection.disconnect();
  });
});
describe("live query cache", () => {
  const message = (data: Partial<LiveMessage>): LiveMessage => ({
    type: "snapshot",
    id: "sub-1",
    topic: "project",
    ...data,
  });
  it("replaces project snapshots and merges aggregate lists without changing other projects", () => {
    const client = new QueryClient();
    client.setQueryData(queries.containers().queryKey, [
      { id: "old", project_id: "p1", name: "old", port: 80 },
      { id: "other", project_id: "p2", name: "other", port: 80 },
    ]);
    const data = {
      project: { id: "p1", name: "App" },
      containers: [{ id: "new", project_id: "p1" }],
      releases: [],
      routes: [],
    };
    applyLiveMessage(
      client,
      { topic: "project", project_id: "p1" },
      message({ data }),
    );
    expect(client.getQueryData(queries.project("p1").queryKey)).toEqual(
      data.project,
    );
    expect(client.getQueryData(queries.containers().queryKey)).toEqual([
      { id: "other", project_id: "p2", name: "other", port: 80 },
      { id: "new", project_id: "p1" },
    ]);
    applyLiveMessage(
      client,
      { topic: "project", project_id: "p1" },
      message({ type: "delete", kind: "container", record_id: "new" }),
    );
    expect(client.getQueryData(queries.containers("p1").queryKey)).toEqual([]);
    client.clear();
  });
  it("appends streamed logs and preserves them on metadata updates, then replaces on reset", () => {
    const client = new QueryClient();
    const sub = { topic: "release_log" as const, release_id: "r1" };
    applyLiveMessage(
      client,
      sub,
      message({ data: { id: "r1", log: "first\n", status: "building" } }),
    );
    applyLiveMessage(client, sub, message({ type: "log", data: "second\n" }));
    applyLiveMessage(
      client,
      sub,
      message({
        type: "upsert",
        kind: "release",
        record: { id: "r1", project_id: "p1", status: "running", created: "" },
      }),
    );
    expect(client.getQueryData(["releases/r1"])).toMatchObject({
      log: "first\nsecond\n",
      status: "running",
    });
    applyLiveMessage(
      client,
      sub,
      message({ type: "log", data: "new build\n", reset: true }),
    );
    expect(client.getQueryData(["releases/r1"])).toMatchObject({
      log: "new build\n",
    });
    client.clear();
  });
  it("keeps traffic windows isolated and updates the system update cache", () => {
    const client = new QueryClient();
    applyLiveMessage(
      client,
      { topic: "traffic", project_id: "p1", window: 900 },
      message({ data: { requests: 5 } }),
    );
    expect(client.getQueryData(["traffic", "p1", 900])).toEqual({
      requests: 5,
    });
    expect(client.getQueryData(["traffic", "p1", 300])).toBeUndefined();
    applyLiveMessage(
      client,
      { topic: "system_update" },
      message({ data: { current: "v2" } }),
    );
    expect(client.getQueryData(queries.updates().queryKey)).toEqual({
      current: "v2",
    });
    client.clear();
  });
});
