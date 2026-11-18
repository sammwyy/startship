import {
  fireEvent,
  render,
  screen,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { OverviewView } from "./OverviewView";

const state = vi.hoisted(() => ({
  editable: true,
  refresh: vi.fn(),
  notify: vi.fn(),
  request: vi.fn(),
  project: {
    id: "p1",
    name: "Storefront",
    workspace_id: "w1",
    git_url: "https://github.com/team/storefront",
    branch: "production",
    auto_deploy: true,
    root_dir: ".",
    build_type: "auto",
    port: 0,
  },
  releases: [
    {
      id: "release-1",
      project_id: "p1",
      status: "running",
      commit: "abcdef123",
      created: "2026-11-17T18:00:00Z",
    },
  ],
}));
vi.mock("../../layouts/ProjectLayout", () => ({
  useProject: () => ({ ...state, containers: [], routes: [] }),
}));
vi.mock("../../providers/SessionProvider", () => ({
  useSession: () => ({ can: () => state.editable }),
}));
vi.mock("../../providers/UIProvider", () => ({
  useUI: () => ({ notify: state.notify }),
}));
vi.mock("../../services/api", () => ({ request: state.request }));

describe("project overview", () => {
  afterEach(cleanup);
  beforeEach(() => {
    vi.clearAllMocks();
    state.editable = true;
  });
  const show = () =>
    render(
      <MemoryRouter>
        <OverviewView />
      </MemoryRouter>,
    );
  it("puts configuration, project information and recent deployments on the landing page", () => {
    show();
    expect(screen.getByRole("heading", { name: "Storefront" })).toBeTruthy();
    expect(screen.getByLabelText("Project name")).toHaveProperty(
      "value",
      "Storefront",
    );
    expect(
      screen.getByRole("heading", { name: "Project information" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: /abcdef1/ }).getAttribute("href"),
    ).toBe("/project/p1/deployments?release=release-1");
    expect(screen.getByText("production")).toBeTruthy();
  });
  it("saves general settings from the overview and refreshes project data", async () => {
    show();
    fireEvent.change(screen.getByLabelText("Project name"), {
      target: { value: "New storefront" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(state.request).toHaveBeenCalledWith(
        "projects/p1",
        {
          name: "New storefront",
          auto_deploy: true,
          root_dir: ".",
        },
        "PATCH",
      ),
    );
    await waitFor(() => expect(state.refresh).toHaveBeenCalled());
  });
  it("keeps the overview readable without configuration write permission", () => {
    state.editable = false;
    show();
    expect(
      screen.getByLabelText("Project name").closest("fieldset")?.disabled,
    ).toBe(true);
    expect(screen.getByRole("link", { name: /abcdef1/ })).toBeTruthy();
  });
});
