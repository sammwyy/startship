import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ProjectLayout } from "../layouts/ProjectLayout";
import { OverviewView } from "../views/project/OverviewView";
import { applyLiveMessage } from "../services/live";
import { queries } from "../services/queries";

const state = vi.hoisted(() => ({
  deploy: true,
  request: vi.fn(),
  notify: vi.fn(),
}));
vi.mock("../providers/LiveProvider", () => ({ useLiveSubscription: vi.fn() }));
vi.mock("../providers/SessionProvider", () => ({
  useSession: () => ({
    can: (permission: string) =>
      permission === "deploy" ? state.deploy : true,
  }),
}));
vi.mock("../providers/UIProvider", () => ({
  useUI: () => ({
    notify: state.notify,
    run: (action: () => Promise<unknown>) => void action().catch(state.notify),
  }),
}));
vi.mock("../services/api", () => ({ request: state.request }));

beforeEach(() => {
  vi.clearAllMocks();
  state.deploy = true;
});
afterEach(cleanup);

function show() {
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity, retry: false } },
  });
  const project = {
    id: "p1",
    name: "Demo",
    workspace_id: "w1",
    sleep_after: 15,
  };
  client.setQueryData(queries.project("p1").queryKey, project);
  client.setQueryData(queries.previews("p1").queryKey, []);
  client.setQueryData(queries.containers("p1").queryKey, [
    {
      id: "container",
      name: "Demo",
      port: 3000,
      project_id: "p1",
      status: "running",
      active_deployment: "release",
    },
  ]);
  client.setQueryData(queries.releases("p1").queryKey, [
    {
      id: "release",
      project_id: "p1",
      status: "running",
      created: "2026-11-19T00:00:00Z",
    },
  ]);
  client.setQueryData(queries.routes("p1").queryKey, [
    { id: "route", host: "demo.test", path_prefix: "/", strip_prefix: false },
  ]);
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/project/p1"]}>
        <Routes>
          <Route path="/project/:projectId" element={<ProjectLayout />}>
            <Route index element={<OverviewView />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  const update = (sleeping: boolean) =>
    act(() => {
      applyLiveMessage(
        client,
        { topic: "project", project_id: "p1" },
        {
          type: "upsert",
          id: "project-sleep",
          topic: "project",
          kind: "project",
          record: { ...project, sleeping },
        },
      );
    });
  return { update };
}

it("re-renders sleep and wake from live project upserts without polling", async () => {
  const { update } = show();
  expect(screen.queryByRole("button", { name: "Wake now" })).toBeNull();
  expect(
    screen.getByText("1 container · 1 running · 1 domain route"),
  ).toBeTruthy();
  update(true);
  fireEvent.click(await screen.findByRole("button", { name: "Wake now" }));
  await waitFor(() =>
    expect(state.request).toHaveBeenCalledWith("projects/p1/wake", {}, "POST"),
  );
  expect(screen.getAllByText(/^sleeping$/i)).toHaveLength(2);
  expect(
    screen.getByText("1 container · sleeping · 1 domain route"),
  ).toBeTruthy();
  expect(
    screen.queryByText("1 container · 1 running · 1 domain route"),
  ).toBeNull();
  expect(screen.getByText("running")).toBeTruthy();
  update(false);
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Wake now" })).toBeNull(),
  );
  expect(screen.getByText("After 15 min")).toBeTruthy();
  expect(
    screen.getByText("1 container · 1 running · 1 domain route"),
  ).toBeTruthy();
  expect(state.request).toHaveBeenCalledTimes(1);
});

it("shows sleeping state without offering wake to viewers", async () => {
  state.deploy = false;
  const { update } = show();
  update(true);
  await waitFor(() =>
    expect(screen.getAllByText(/^sleeping$/i)).toHaveLength(2),
  );
  expect(screen.queryByRole("button", { name: "Wake now" })).toBeNull();
});

it("reports a failed wake and keeps the sleeping controls available", async () => {
  state.request.mockRejectedValueOnce(new Error("Wake failed"));
  const { update } = show();
  update(true);
  fireEvent.click(await screen.findByRole("button", { name: "Wake now" }));
  await waitFor(() =>
    expect(state.notify).toHaveBeenCalledWith(new Error("Wake failed")),
  );
  expect(screen.getByRole("button", { name: "Wake now" })).toBeEnabled();
});
