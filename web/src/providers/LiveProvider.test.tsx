import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { reloadDashboard } from "../lib/dashboardReload";
import type { SystemVersion } from "../services/types";
import { LiveProvider } from "./LiveProvider";

const live = vi.hoisted(() => ({
  ready: (_version: SystemVersion) => {},
}));
vi.mock("../lib/dashboardReload", () => ({ reloadDashboard: vi.fn() }));
vi.mock("../services/live", () => ({
  LiveConnection: class {
    constructor(
      _onMessage: unknown,
      _onStatus: unknown,
      onReady: typeof live.ready,
    ) {
      live.ready = onReady;
    }
    connect() {}
    disconnect() {}
  },
}));

it.each([
  { version: "v1.1.0", commit: "first" },
  { version: "v1.0.0", commit: "second" },
])("reloads when the connected daemon build changes to %o", (changed) => {
  const client = new QueryClient();
  render(
    <QueryClientProvider client={client}>
      <LiveProvider>Dashboard</LiveProvider>
    </QueryClientProvider>,
  );
  act(() => live.ready({ version: "v1.0.0", commit: "first" }));
  act(() => live.ready({ version: "v1.0.0", commit: "first" }));
  expect(reloadDashboard).not.toHaveBeenCalled();
  act(() => live.ready(changed));
  expect(reloadDashboard).toHaveBeenCalledOnce();
});
