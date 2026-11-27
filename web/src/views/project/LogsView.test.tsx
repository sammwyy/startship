import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { LiveProvider } from "../../providers/LiveProvider";
import { LogsView } from "./LogsView";

const state = vi.hoisted(() => ({
  containers: [
    { id: "web", name: "Web", status: "running" },
    { id: "worker", name: "Worker", status: "starting" },
  ],
  request: vi.fn(async (path: string) => ({
    log: path === "containers/web/logs" ? "HTTP runtime output\n" : "",
  })),
}));
vi.mock("../../layouts/ProjectLayout", () => ({
  useProject: () => ({
    project: { id: "p1" },
    containers: state.containers,
    releases: [{ id: "release-1" }],
  }),
}));
vi.mock("../../components/ProjectHeading", () => ({
  ProjectHeading: ({ title }: { title: string }) => <h1>{title}</h1>,
}));
vi.mock("../../services/api", () => ({ request: state.request }));
vi.mock("../../providers/UIProvider", () => ({
  useUI: () => ({ run: vi.fn(), notify: vi.fn() }),
}));

class Socket {
  static OPEN = 1;
  static current: Socket;
  readyState = Socket.OPEN;
  send = vi.fn<(message: string) => void>();
  close = vi.fn();
  onmessage?: (event: MessageEvent) => void;
  constructor() {
    Socket.current = this;
  }
  receive(message: object) {
    act(() =>
      this.onmessage?.(
        new MessageEvent("message", {
          data: JSON.stringify(message),
        }),
      ),
    );
  }
}

function Location() {
  return <output>{useLocation().pathname}</output>;
}

function openLogs(containerPath = "") {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <LiveProvider>
        <MemoryRouter initialEntries={[`/project/p1/logs${containerPath}`]}>
          <Routes>
            <Route
              path="/project/:projectId/logs/:containerId?"
              element={
                <>
                  <LogsView />
                  <Location />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </LiveProvider>
    </QueryClientProvider>,
  );
  Socket.current.receive({ type: "ready" });
  return Socket.current;
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("WebSocket", Socket);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("loads runtime logs by default, appends live output, and switches containers", async () => {
  const socket = openLogs();
  const selector = screen.getByRole("combobox", { name: "Container" });
  expect(selector).toHaveValue("web");
  expect(
    screen.getAllByRole("option").map((option) => option.textContent),
  ).toEqual(["Web", "Worker"]);
  await screen.findByText("HTTP runtime output");
  const subscription = JSON.parse(socket.send.mock.calls[0][0]);
  expect(subscription).toMatchObject({
    type: "subscribe",
    topic: "container_log",
    container_id: "web",
  });
  socket.receive({
    id: subscription.id,
    topic: "container_log",
    type: "log",
    data: "Live runtime output\n",
  });
  await screen.findByText(/Live runtime output/);
  expect(screen.getByLabelText("Project logs").textContent).toBe(
    "HTTP runtime output\nLive runtime output\n",
  );

  fireEvent.change(selector, { target: { value: "worker" } });
  expect(screen.getByText("/project/p1/logs/worker")).toBeVisible();
  await screen.findByText("No runtime output yet.");
  expect(state.request.mock.calls.map(([path]) => path)).toEqual([
    "containers/web/logs",
    "containers/worker/logs",
  ]);
  expect(
    socket.send.mock.calls.map(([message]) => JSON.parse(message)),
  ).toEqual([
    subscription,
    { type: "unsubscribe", id: subscription.id },
    expect.objectContaining({
      type: "subscribe",
      topic: "container_log",
      container_id: "worker",
    }),
  ]);
});

it.each([
  ["/worker", "worker"],
  ["/removed", "web"],
])("selects an available container for %s", (path, expected) => {
  openLogs(path);
  expect(screen.getByRole("combobox", { name: "Container" })).toHaveValue(
    expected,
  );
  expect(state.request.mock.calls.map(([path]) => path)).toEqual([
    `containers/${expected}/logs`,
  ]);
});

it("has no log source when a project has deployments but no containers", () => {
  const containers = state.containers;
  state.containers = [];
  try {
    const socket = openLogs();
    expect(screen.getByText("No containers yet")).toBeVisible();
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(state.request).not.toHaveBeenCalled();
    expect(socket.send).not.toHaveBeenCalled();
  } finally {
    state.containers = containers;
  }
});
