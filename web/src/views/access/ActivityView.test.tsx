import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ScopedActivityTabs } from "../../components/ScopedActivityTabs";
import { ActivityView } from "./ActivityView";

const scope = vi.hoisted(() => ({
  workspace: { id: "w1", name: "Team", permissions: ["activity.view"] },
  project: { id: "p1", name: "Storefront", permissions: ["activity.view"] },
  liveFeed: vi.fn(() => ({ data: [] })),
}));
vi.mock("../../layouts/DashboardLayout", () => ({
  useDashboard: () => ({
    workspaces: [scope.workspace],
    project: scope.project,
  }),
}));
vi.mock("../../providers/SessionProvider", () => ({
  useSession: () => ({
    can: (permission: string, resource?: { permissions: string[] }) =>
      !!resource?.permissions.includes(permission),
  }),
}));
vi.mock("../../providers/UIProvider", () => ({ useUI: () => ({}) }));
vi.mock("../../providers/LiveProvider", () => ({
  useLiveFeed: scope.liveFeed,
}));

describe("scoped activity navigation", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it.each([
    [
      "workspace",
      "/workspace/w1",
      "Projects",
      "Audit Activity",
      "workspace_id",
    ],
    [
      "project",
      "/project/p1/overview",
      "Overview",
      "Activity Log",
      "project_id",
    ],
  ] as const)(
    "opens the live %s feed and returns to its home tab",
    (kind, home, label, title, idKey) => {
      const activity = `/${kind}/${scope[kind].id}/activity`;
      render(
        <MemoryRouter initialEntries={[home]}>
          <Routes>
            <Route
              path={home}
              element={
                <ScopedActivityTabs scope={kind} resource={scope[kind]} />
              }
            />
            <Route
              path={`/${kind}/:${kind}Id/activity`}
              element={<ActivityView />}
            />
          </Routes>
        </MemoryRouter>,
      );
      expect(
        screen.getByRole("link", { name: label }).getAttribute("aria-current"),
      ).toBe("page");
      const link = screen.getByRole("link", { name: title });
      expect(link.getAttribute("href")).toBe(activity);
      fireEvent.click(link);
      expect(screen.getByRole("heading", { name: title })).toBeTruthy();
      expect(
        screen.getByRole("link", { name: title }).getAttribute("aria-current"),
      ).toBe("page");
      expect(scope.liveFeed).toHaveBeenCalledWith({
        topic: `${kind}_activity`,
        [idKey]: scope[kind].id,
      });
      fireEvent.click(screen.getByRole("link", { name: label }));
      expect(
        screen.getByRole("link", { name: label }).getAttribute("aria-current"),
      ).toBe("page");
    },
  );

  it("does not show an activity tab without the scoped permission", () => {
    render(
      <MemoryRouter>
        <ScopedActivityTabs
          scope="workspace"
          resource={{ id: "w1", permissions: [] }}
        />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("link", { name: "Audit Activity" })).toBeNull();
  });
});
