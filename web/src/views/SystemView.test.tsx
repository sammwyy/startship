import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UIProvider } from "../providers/UIProvider";
import { ApiError, request } from "../services/api";
import { SystemView } from "./SystemView";
import type { UpdateInfo } from "../services/types";

const session = vi.hoisted(() => ({ user: { platform_admin: true } }));
vi.mock("../providers/SessionProvider", () => ({ useSession: () => session }));
vi.mock("../services/api", async (original) => ({
  ...(await original<typeof import("../services/api")>()),
  request: vi.fn(),
}));
let info: UpdateInfo;
let installed = "v1.0.0";
function renderView() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  const result = render(
    <QueryClientProvider client={client}>
      <UIProvider>
        <SystemView />
      </UIProvider>
    </QueryClientProvider>,
  );
  return { client, ...result };
}
beforeEach(() => {
  session.user.platform_admin = true;
  installed = "v1.0.0";
  info = {
    current: "v1.0.0",
    commit: "first",
    latest: "v1.1.0",
    update_available: true,
    development: false,
    self_update: true,
    notes: "<script>alert('safe text')</script>",
    release_url: "https://github.com/sammwyy/startship/releases/tag/v1.1.0",
  };
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
  HTMLDialogElement.prototype.close = function () {
    this.open = false;
  };
  vi.mocked(request).mockImplementation(async (path, body) => {
    if (path === "system/version")
      return { version: installed, commit: "first" };
    if (body) return { version: "v1.1.0" };
    return info;
  });
});
describe("system updates", () => {
  it("checks on demand and safely shows release notes", async () => {
    const { container } = renderView();
    await screen.findByText("v1.1.0 is available");
    await userEvent.click(screen.getByText("Check for updates"));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("system/update?refresh=1"),
    );
    expect(container.querySelector("script")).toBeNull();
    expect(screen.getByText("Release notes")).toHaveAttribute(
      "href",
      info.release_url,
    );
  });
  it("requires confirmation, queues one update and confirms the changed daemon version", async () => {
    const { client } = renderView();
    const trigger = await screen.findByText("Update daemon and CLI");
    await userEvent.click(trigger);
    expect(request).not.toHaveBeenCalledWith(
      "system/update",
      expect.anything(),
    );
    expect(screen.getByRole("dialog")).toHaveTextContent("restarts the daemon");
    await userEvent.click(screen.getByText("Update and restart"));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("system/update", {
        version: "v1.1.0",
      }),
    );
    expect(trigger).toBeDisabled();
    installed = "v1.1.0";
    act(() =>
      client.setQueryData(["system/version"], {
        version: installed,
        commit: "updated",
      }),
    );
    expect(
      await screen.findByText(/v1.1.0 is running/, {}, { timeout: 4000 }),
    ).toBeInTheDocument();
    expect(screen.getByText("Reload dashboard")).toBeInTheDocument();
  });
  it.each([
    "member",
    "unsupported",
    "development",
    "running",
    "pending",
    "check-error",
  ])("prevents updates for %s state", async (state) => {
    if (state === "member") session.user.platform_admin = false;
    if (state === "unsupported") info.self_update = false;
    if (state === "development") {
      info.development = true;
      info.update_available = false;
    }
    if (state === "running")
      info.update = { state: "running", version: "v1.1.0" };
    if (state === "pending") info.pending = { version: "v1.1.0" };
    if (state === "check-error")
      info.check_error = "Release server unavailable";
    renderView();
    expect(await screen.findByText("Update daemon and CLI")).toBeDisabled();
    if (state === "unsupported")
      expect(screen.getByText("sudo startship update")).toBeInTheDocument();
    expect(
      vi.mocked(request).mock.calls.filter(([, body]) => body),
    ).toHaveLength(0);
  });
  it("handles a daemon without the update API", async () => {
    vi.mocked(request).mockRejectedValue(new ApiError("not found", 404));
    renderView();
    expect(
      await screen.findByText(/not supported by this daemon/),
    ).toBeInTheDocument();
    expect(screen.queryByText("Update daemon and CLI")).not.toBeInTheDocument();
  });
  it("keeps a failed update visible and rejects unsafe release URLs", async () => {
    info.release_url = "javascript:alert('unsafe')";
    info.update = {
      state: "failed",
      version: "v1.1.0",
      error: "Checksum mismatch",
    };
    renderView();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Checksum mismatch",
    );
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
