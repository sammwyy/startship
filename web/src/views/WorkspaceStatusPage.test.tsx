import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { request } from "../services/api";
import { StatusPageForm } from "./WorkspaceSettingsView";

vi.mock("../services/api", () => ({ request: vi.fn().mockResolvedValue({}) }));
afterEach(cleanup);

it("publishes only the projects picked in the workspace form", async () => {
  const user = userEvent.setup();
  render(
    <StatusPageForm
      id="ws"
      settings={{ enabled: false, slug: "", title: "", projects: [] }}
      projects={[
        { id: "api", name: "API", workspace_id: "ws", uptime_monitoring: true },
        { id: "private", name: "Private", workspace_id: "ws" },
      ]}
      onSaved={() => {}}
    />,
  );
  await user.click(
    screen.getByRole("checkbox", { name: "Publish status page" }),
  );
  await user.type(screen.getByLabelText("Page title"), "Product status");
  await user.type(screen.getByLabelText("Address slug"), "product-status");
  await user.click(screen.getByRole("checkbox", { name: /API/ }));
  await user.click(screen.getByRole("button", { name: "Save status page" }));
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith(
      "workspaces/ws/status-page",
      {
        enabled: true,
        slug: "product-status",
        title: "Product status",
        projects: ["api"],
      },
      "PATCH",
    ),
  );
});
