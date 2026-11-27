import {
  QueryClient,
  QueryClientProvider,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { request } from "../services/api";
import { queries } from "../services/queries";
import { DashboardLayout } from "./DashboardLayout";
import { WorkspacesView } from "../views/WorkspacesView";
import type { Workspace } from "../services/types";

vi.mock("../services/api", () => ({ request: vi.fn() }));

vi.mock("../providers/SessionProvider", () => ({
  useSession: () => ({ user: { username: "member" }, can: () => false }),
}));
vi.mock("../providers/UIProvider", () => ({
  useUI: () => ({ navigationOpen: false, setNavigation: vi.fn() }),
}));
vi.mock("../providers/LiveProvider", () => ({
  useLiveStatus: () => "connected",
  useLiveSubscription: vi.fn(),
}));

const dialogClose = HTMLDialogElement.prototype.close;
afterEach(() => {
  HTMLDialogElement.prototype.close = dialogClose;
});

it("shows workspaces skeletons instead of an empty state until the list resolves", async () => {
  HTMLDialogElement.prototype.close = vi.fn();
  let resolve: (workspaces: Workspace[]) => void = () => {};
  vi.mocked(request).mockImplementation(
    (path) =>
      (path === "workspaces"
        ? new Promise<Workspace[]>((done) => (resolve = done))
        : Promise.resolve({})) as ReturnType<typeof request>,
  );
  const client = new QueryClient();
  const { container } = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspaces"]}>
        <Routes>
          <Route element={<DashboardLayout />}>
            <Route path="/workspaces" element={<WorkspacesView />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(
    screen.getByRole("status", { name: "Loading workspaces…" }),
  ).toBeVisible();
  expect(container.querySelectorAll(".skeleton-card")).toHaveLength(3);
  expect(screen.queryByText("No workspaces yet")).not.toBeInTheDocument();
  await act(async () => resolve([]));
  expect(await screen.findByText("No workspaces yet")).toBeVisible();
});

it("keeps the dashboard sidebar visible when outlet data suspends", async () => {
  HTMLDialogElement.prototype.close = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  });
  client.setQueryData(queries.workspaces().queryKey, []);
  client.setQueryData(queries.updates().queryKey, {
    current: "0.1.0",
    commit: "abc",
    development: false,
    self_update: false,
    update_available: false,
  });
  let finish: (value: string) => void = () => {};
  const pending = new Promise<string>((resolve) => (finish = resolve));
  function WorkspaceContent() {
    const { data } = useSuspenseQuery({
      queryKey: ["delayed"],
      queryFn: () => pending,
    });
    return <h1>{data}</h1>;
  }
  const { container } = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspace/studio"]}>
        <Routes>
          <Route element={<DashboardLayout />}>
            <Route
              path="/workspace/:workspaceId"
              element={<WorkspaceContent />}
            />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(
    screen.getByRole("complementary", { name: "Dashboard sidebar" }),
  ).toBeVisible();
  expect(screen.getByRole("status", { name: "Loading…" })).toBeVisible();
  expect(container.querySelectorAll(".skeleton-stat")).toHaveLength(3);
  await act(async () => finish("Projects"));
  expect(
    await screen.findByRole("heading", { name: "Projects" }),
  ).toBeVisible();
});

it("omits the live connection footer but shows workspace selection and update availability as text", () => {
  HTMLDialogElement.prototype.close = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  });
  client.setQueryData(queries.workspaces().queryKey, [
    { id: "studio", name: "Studio" },
    { id: "team", name: "Team" },
  ]);
  client.setQueryData(queries.updates().queryKey, {
    current: "0.1.0",
    commit: "abc",
    development: false,
    self_update: true,
    update_available: true,
  });

  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspace/studio"]}>
        <DashboardLayout />
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(screen.queryByText("Live updates connected")).not.toBeInTheDocument();
  expect(screen.getByText("Current").closest("a")).toHaveAttribute(
    "aria-current",
    "true",
  );
  expect(screen.getByText("System: update available")).toBeVisible();
});
