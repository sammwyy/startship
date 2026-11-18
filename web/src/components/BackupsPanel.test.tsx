import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { request } from "../services/api";
import { BackupsPanel, ScheduleEditor, scheduleLabel } from "./BackupsPanel";
import type { Backup, Service } from "../services/types";
vi.mock("../services/api", () => ({ request: vi.fn(), remove: vi.fn() }));
vi.mock("../providers/LiveProvider", () => ({
  useLiveSubscription: () => undefined,
}));
const ui = vi.hoisted(() => ({
  notify: vi.fn(),
  run: (action: () => Promise<unknown>) => void action(),
  dialog: vi.fn(),
}));
vi.mock("../providers/UIProvider", () => ({ useUI: () => ui }));
const service = {
  id: "s",
  name: "db",
  engine: "postgresql",
  version: "17",
  volume_id: "v",
  status: "running",
} as Service;
const backup: Backup = {
  id: "b",
  workspace_id: "w",
  service_id: "s",
  service_name: "db",
  engine: "postgresql",
  version: "17",
  volume_id: "bv",
  file: "s/b.tar.gz",
  size_bytes: 2048,
  status: "completed",
  trigger: "scheduled",
  created: "2026-11-18T03:00:00Z",
  finished: "2026-11-18T03:00:02Z",
  created_by: "",
};
beforeAll(() => {
  for (const method of ["showPopover", "hidePopover"])
    Object.defineProperty(HTMLElement.prototype, method, {
      configurable: true,
      value: vi.fn(),
    });
});
beforeEach(() => {
  vi.mocked(request).mockReset();
  vi.mocked(request).mockImplementation(async (path) =>
    path === "workspaces/w/backups" ? { backups: [backup], volume: null } : {},
  );
  ui.dialog.mockReset();
});
const wrap = (node: React.ReactNode) =>
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
describe("backups", () => {
  it("labels schedules", () => {
    expect(scheduleLabel()).toBe("Not scheduled");
    expect(
      scheduleLabel({
        enabled: true,
        frequency: "weekly",
        weekday: 1,
        hour: 4,
        minute: 5,
        keep: 3,
      }),
    ).toBe("Mondays at 04:05 UTC");
  });
  it("lists backups and starts a restore", async () => {
    wrap(
      <BackupsPanel
        workspaceId="w"
        services={[service]}
        service={service}
        allowed
      />,
    );
    expect(await screen.findByText(/Scheduled · postgresql 17 · 2.0 KB/))
      .toBeTruthy;
    await userEvent.click(screen.getByRole("button", { name: /Restore/ }));
    const options = ui.dialog.mock.calls[0][0];
    await options.onSubmit(new FormData());
    expect(request).toHaveBeenCalledWith("backups/b/restore", {
      service_id: "s",
    });
    await userEvent.click(screen.getByRole("button", { name: "Back up now" }));
    expect(request).toHaveBeenCalledWith("services/s/backups", {});
  });
  it("hides management actions without permission", async () => {
    wrap(
      <BackupsPanel
        workspaceId="w"
        services={[service]}
        service={service}
        allowed={false}
      />,
    );
    await screen.findByText(/Scheduled/);
    expect(screen.queryByRole("button", { name: /Restore/ })).toBeNull();
    expect(screen.queryByRole("link", { name: /Download/ })).toBeNull();
  });
  it("saves a schedule", async () => {
    wrap(<ScheduleEditor service={service} />);
    const save = screen.getByRole("button", { name: "Save schedule" });
    expect((save as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(
      screen.getByRole("switch", { name: "Scheduled backups" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Weekly" }));
    await userEvent.click(save);
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "services/s/backup-schedule",
        {
          enabled: true,
          frequency: "weekly",
          hour: 3,
          minute: 0,
          weekday: 0,
          keep: 7,
        },
        "PUT",
      ),
    );
  });
});
