import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { DeploymentsView } from "./DeploymentsView";
const state = vi.hoisted(() => ({
  useLog: vi.fn(() => ({
    data: { log: "Build output", status: "running" },
    isFetching: false,
    error: null,
  })),
}));
vi.mock("../../components/LogPanel", () => ({
  useLog: state.useLog,
  LogPanel: () => <pre>Build output</pre>,
}));
vi.mock("../../components/ProjectHeading", () => ({
  ProjectHeading: () => <h1>Deployments</h1>,
}));
vi.mock("../../layouts/ProjectLayout", () => ({
  useProject: () => ({
    project: { id: "p1" },
    releases: [
      { id: "newest", status: "running", created: "2026-11-17" },
      { id: "older", status: "failed", created: "2026-11-16" },
    ],
    refresh: vi.fn(),
  }),
}));
vi.mock("../../providers/SessionProvider", () => ({
  useSession: () => ({ can: () => true }),
}));
vi.mock("../../providers/UIProvider", () => ({
  useUI: () => ({ run: vi.fn() }),
}));
afterEach(cleanup);
describe("deployment history", () => {
  it("opens the selected deployment linked from the overview and supports selection", () => {
    render(
      <MemoryRouter initialEntries={["/project/p1/deployments?release=older"]}>
        <DeploymentsView />
      </MemoryRouter>,
    );
    expect(state.useLog).toHaveBeenLastCalledWith("older");
    expect(screen.queryByText("Project releases")).toBeNull();
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
});
