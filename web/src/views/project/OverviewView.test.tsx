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
    sleep_after: 0,
    sleeping: false,
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
    state.project.sleep_after = 0;
    state.project.sleeping = false;
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
          sleep_after: 0,
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
  it("saves enabled idle sleep in minutes, defaulting to 15", async () => {
    show();
    expect(
      screen.queryByRole("spinbutton", { name: "Idle minutes" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("checkbox", { name: "Sleep when idle" }));
    expect(
      screen.getByRole("spinbutton", { name: "Idle minutes" }),
    ).toHaveProperty("value", "15");
    fireEvent.change(screen.getByRole("spinbutton", { name: "Idle minutes" }), {
      target: { value: "25" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(state.request).toHaveBeenCalledWith(
        "projects/p1",
        expect.objectContaining({ sleep_after: 25 }),
        "PATCH",
      ),
    );
    await waitFor(() =>
      expect(state.notify).toHaveBeenCalledWith(
        "Project saved; idle sleep settings apply immediately",
      ),
    );
  });
  it("saves zero when idle sleep is disabled", async () => {
    state.project.sleep_after = 30;
    show();
    expect(
      screen.getByRole("spinbutton", { name: "Idle minutes" }),
    ).toHaveProperty("value", "30");
    expect(screen.getByText("After 30 min")).toBeTruthy();
    fireEvent.click(screen.getByRole("checkbox", { name: "Sleep when idle" }));
    expect(
      screen.queryByRole("spinbutton", { name: "Idle minutes" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(state.request).toHaveBeenCalledWith(
        "projects/p1",
        expect.objectContaining({ sleep_after: 0 }),
        "PATCH",
      ),
    );
  });
  it("shows save errors without claiming settings were applied", async () => {
    state.request.mockRejectedValueOnce(new Error("Could not save"));
    show();
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not save",
    );
    expect(state.notify).not.toHaveBeenCalled();
  });
});
