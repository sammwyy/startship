import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { queries } from "../services/queries";
import { DashboardLayout } from "./DashboardLayout";

vi.mock("../providers/SessionProvider", () => ({
  useSession: () => ({ user: { username: "member" }, can: () => false }),
}));
vi.mock("../providers/UIProvider", () => ({
  useUI: () => ({ navigationOpen: false, setNavigation: vi.fn() }),
}));
vi.mock("../providers/LiveProvider", () => ({
  useLiveStatus: () => "connected",
  useLiveSubscription: vi.fn(),
}));

const dialogClose = HTMLDialogElement.prototype.close;
afterEach(() => {
  HTMLDialogElement.prototype.close = dialogClose;
});

it("omits the live connection footer but shows workspace selection and update availability as text", () => {
  HTMLDialogElement.prototype.close = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  });
  client.setQueryData(queries.workspaces().queryKey, [
    { id: "studio", name: "Studio" },
    { id: "team", name: "Team" },
  ]);
  client.setQueryData(queries.updates().queryKey, {
    current: "0.1.0",
    commit: "abc",
    development: false,
    self_update: true,
    update_available: true,
  });

  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/workspace/studio"]}>
        <DashboardLayout />
      </MemoryRouter>
    </QueryClientProvider>,
  );

  expect(screen.queryByText("Live updates connected")).not.toBeInTheDocument();
  expect(screen.getByText("Current").closest("a")).toHaveAttribute(
    "aria-current",
    "true",
  );
  expect(screen.getByText("System: update available")).toBeVisible();
});
