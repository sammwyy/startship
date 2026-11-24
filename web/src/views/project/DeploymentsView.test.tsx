import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { DeploymentsView } from "./DeploymentsView";
const state = vi.hoisted(() => ({
  sleeping: false,
  preview: false,
  canDeploy: true,
  request: vi.fn(async () => ({})),
  refresh: vi.fn(),
  notify: vi.fn(),
  useLog: vi.fn(() => ({
    data: { log: "Build output", status: "running", redeployable: true },
    isFetching: false,
    error: null,
  })),
}));
vi.mock("../../components/LogPanel", () => ({
  useLog: state.useLog,
  LogPanel: () => <pre>Build output</pre>,
}));
vi.mock("../../components/ProjectHeading", () => ({
  ProjectHeading: ({ actions }: { actions: ReactNode }) => (
    <header>
      <h1>Deployments</h1>
      {actions}
    </header>
  ),
}));
vi.mock("../../services/api", () => ({ request: state.request }));
vi.mock("../../layouts/ProjectLayout", () => ({
  useProject: () => ({
    project: {
      id: "p1",
      git_url: "https://example.com/repo.git",
      sleeping: state.sleeping,
      active_deployment: "newest",
    },
    releases: [
      {
        id: "newest",
        preview_id: state.preview ? "pv" : undefined,
        branch: "feature-x",
        status: "running",
        created: "2026-11-17",
        container_releases: [{ id: "c1" }],
      },
      { id: "older", status: "failed", created: "2026-11-16" },
    ],
    refresh: state.refresh,
  }),
}));
vi.mock("../../providers/SessionProvider", () => ({
  useSession: () => ({ can: () => state.canDeploy }),
}));
vi.mock("../../providers/UIProvider", () => ({
  useUI: () => ({
    run: (action: () => Promise<unknown>) => action(),
    notify: state.notify,
  }),
}));
beforeEach(() => {
  vi.clearAllMocks();
  state.canDeploy = true;
  state.sleeping = false;
  state.preview = false;
});
afterEach(cleanup);
describe("deployment history", () => {
  it("tags preview releases and hides redeploy even if the log marks it redeployable", () => {
    state.preview = true;
    render(
      <MemoryRouter>
        <DeploymentsView />
      </MemoryRouter>,
    );
    expect(screen.getByText("Preview · feature-x")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Redeploy selected" }),
    ).toBeNull();
  });
  it("shows only the sleeping active release with the sleeping badge", () => {
    state.sleeping = true;
    render(
      <MemoryRouter>
        <DeploymentsView />
      </MemoryRouter>,
    );
    expect(
      screen
        .getByRole("button", { name: /newest/ })
        .querySelector(".badge.sleeping"),
    ).toHaveTextContent("sleeping");
    expect(
      screen
        .getByRole("button", { name: /older/ })
        .querySelector(".badge.failed"),
    ).toHaveTextContent("failed");
  });
  it("opens the selected deployment linked from the overview and supports selection", () => {
    render(
      <MemoryRouter initialEntries={["/project/p1/deployments?release=older"]}>
        <DeploymentsView />
      </MemoryRouter>,
    );
    expect(state.useLog).toHaveBeenLastCalledWith("older");
    expect(screen.queryByText("Project releases")).toBeNull();
    expect(screen.getByText("1 container")).toBeTruthy();
    expect(screen.getByText("0 containers")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /newest/ }));
    expect(state.useLog).toHaveBeenLastCalledWith("newest");
  });
  it("falls back to the latest deployment for a removed bookmark", () => {
    render(
      <MemoryRouter
        initialEntries={["/project/p1/deployments?release=removed"]}
      >
        <DeploymentsView />
      </MemoryRouter>,
    );
    expect(state.useLog).toHaveBeenLastCalledWith("newest");
  });
  it("keeps build controls in the header and sends cache choice only to new builds", async () => {
    render(
      <MemoryRouter initialEntries={["/project/p1/deployments?release=older"]}>
        <DeploymentsView />
      </MemoryRouter>,
    );
    const header = screen.getByRole("banner");
    fireEvent.click(
      within(header).getByRole("checkbox", { name: "Build without cache" }),
    );
    fireEvent.click(within(header).getByRole("button", { name: "Deploy" }));
    await waitFor(() =>
      expect(state.request).toHaveBeenCalledWith("projects/p1/deploy", {
        no_cache: true,
      }),
    );
    await waitFor(() => expect(state.refresh).toHaveBeenCalled());
    await waitFor(() =>
      expect(
        within(header).getByRole("button", { name: "Deploy" }),
      ).not.toBeDisabled(),
    );
    expect(
      screen.getByRole("region", { name: "Deployment history" }),
    ).not.toContainElement(screen.getByRole("checkbox"));
  });
  it("redeploys the selected release instead of starting a new build", async () => {
    render(
      <MemoryRouter initialEntries={["/project/p1/deployments?release=older"]}>
        <DeploymentsView />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Redeploy selected" }));
    await waitFor(() =>
      expect(state.request).toHaveBeenCalledWith("projects/p1/redeploy", {
        deployment_id: "older",
      }),
    );
    await waitFor(() =>
      expect(state.notify).toHaveBeenCalledWith("Project deployment queued"),
    );
  });
  it("preserves read-only deployment permissions after moving the controls", () => {
    state.canDeploy = false;
    render(
      <MemoryRouter>
        <DeploymentsView />
      </MemoryRouter>,
    );
    expect(screen.getByRole("button", { name: "Deploy" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Redeploy selected" }),
    ).toBeDisabled();
    expect(screen.getByRole("checkbox")).toBeDisabled();
  });
});
