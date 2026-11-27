import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, expect, it, vi } from "vitest";
import { useLiveFeed } from "../../providers/LiveProvider";
import { UIProvider } from "../../providers/UIProvider";
import { request } from "../../services/api";
import { NotificationSettings } from "./NotificationSettings";

vi.mock("../../services/api", () => ({
  request: vi.fn().mockResolvedValue({}),
}));
vi.mock("../../providers/LiveProvider", () => ({ useLiveFeed: vi.fn() }));
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
  HTMLDialogElement.prototype.close = function () {
    this.open = false;
  };
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const channel = {
  id: "receiver",
  name: "Team alerts",
  kind: "slack",
  host: "https://hooks.example",
  enabled: true,
  events: ["deploy.failed"],
};
const base = "workspaces/ws/notifications/channels";
const settings = (
  <UIProvider>
    <NotificationSettings id="ws" />
  </UIProvider>
);

it("opts in to selected events when adding a channel", async () => {
  vi.mocked(useLiveFeed).mockReturnValue({
    data: { channels: [], deliveries: [] },
    status: "connected",
  });
  const user = userEvent.setup();
  render(settings);
  expect(useLiveFeed).toHaveBeenCalledWith({
    topic: "notifications",
    workspace_id: "ws",
  });
  expect(
    screen.getByRole("checkbox", { name: "Enable automatic delivery" }),
  ).not.toBeChecked();
  await user.type(screen.getByLabelText("Channel name"), "Team alerts");
  await user.selectOptions(screen.getByLabelText("Destination type"), "slack");
  await user.type(
    screen.getByLabelText("Webhook URL"),
    "https://hooks.example/private",
  );
  await user.click(screen.getByRole("checkbox", { name: "Deployment failed" }));
  await user.click(
    screen.getByRole("checkbox", { name: "Enable automatic delivery" }),
  );
  await user.click(screen.getByRole("button", { name: "Add channel" }));
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith(
      base,
      {
        name: "Team alerts",
        kind: "slack",
        url: "https://hooks.example/private",
        events: ["deploy.failed"],
        enabled: true,
      },
      "POST",
    ),
  );
});

it("edits, tests and deletes a masked channel and shows live delivery results", async () => {
  const data = { channels: [channel], deliveries: [] };
  vi.mocked(useLiveFeed).mockReturnValue({ data, status: "connected" });
  const user = userEvent.setup();
  const view = render(settings);
  const summary = screen.getByText("Team alerts");
  await user.click(summary);
  const editor = within(summary.closest("details")!);
  expect(editor.getByLabelText("Replace webhook URL (optional)")).toHaveValue(
    "",
  );
  expect(editor.getByText("https://hooks.example")).toBeInTheDocument();
  await user.click(
    editor.getByRole("checkbox", { name: "Enable automatic delivery" }),
  );
  await user.click(editor.getByRole("button", { name: "Save channel" }));
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith(
      `${base}/receiver`,
      {
        name: "Team alerts",
        kind: "slack",
        events: ["deploy.failed"],
        enabled: false,
      },
      "PATCH",
    ),
  );
  await user.click(editor.getByRole("button", { name: "Send test" }));
  expect(request).toHaveBeenCalledWith(
    `${base}/receiver/test`,
    undefined,
    "POST",
  );
  vi.mocked(useLiveFeed).mockReturnValue({
    data: {
      ...data,
      deliveries: [
        {
          id: 1,
          channel_id: "receiver",
          event: "test",
          status: "ok",
          http_status: 204,
          created_at: "2026-04-20T12:00:00Z",
          error: "",
        },
      ],
    },
    status: "connected",
  });
  view.rerender(
    <UIProvider>
      <NotificationSettings id="ws" />
    </UIProvider>,
  );
  expect(screen.getByText("Test notification")).toBeInTheDocument();
  expect(screen.getByText(/Delivered · HTTP 204/)).toBeInTheDocument();
  await user.click(editor.getByRole("button", { name: "Delete channel" }));
  const dialog = screen.getByRole("dialog", {
    name: "Delete notification channel?",
  });
  expect(within(dialog).getByText("Team alerts")).toBeInTheDocument();
  expect(request).not.toHaveBeenCalledWith(
    `${base}/receiver`,
    undefined,
    "DELETE",
  );
  await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(request).not.toHaveBeenCalledWith(
    `${base}/receiver`,
    undefined,
    "DELETE",
  );
  await user.click(editor.getByRole("button", { name: "Delete channel" }));
  await user.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: "Delete channel",
    }),
  );
  expect(request).toHaveBeenCalledWith(`${base}/receiver`, undefined, "DELETE");
});

it("replaces the webhook URL only when the editor supplies one", async () => {
  vi.mocked(useLiveFeed).mockReturnValue({
    data: { channels: [channel], deliveries: [] },
    status: "connected",
  });
  const user = userEvent.setup();
  render(settings);
  const summary = screen.getByText("Team alerts");
  await user.click(summary);
  const editor = within(summary.closest("details")!);
  const name = editor.getByLabelText("Channel name");
  await user.clear(name);
  await user.type(name, "Updated alerts");
  const url = editor.getByLabelText("Replace webhook URL (optional)");
  expect(url).toHaveValue("");
  await user.type(url, "https://hooks.example/new-private-path");
  await user.click(editor.getByRole("checkbox", { name: "Deployment failed" }));
  await user.click(editor.getByRole("checkbox", { name: "Uptime recovered" }));
  await user.click(editor.getByRole("button", { name: "Save channel" }));
  await waitFor(() => expect(url).toHaveValue(""));
  expect(request).toHaveBeenCalledWith(
    `${base}/receiver`,
    {
      name: "Updated alerts",
      kind: "slack",
      url: "https://hooks.example/new-private-path",
      events: ["uptime.recovered"],
      enabled: true,
    },
    "PATCH",
  );
});
