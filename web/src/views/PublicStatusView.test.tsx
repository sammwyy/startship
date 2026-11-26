import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { PublicStatusView } from "./PublicStatusView";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("shows public service history and a current incident without credentials", async () => {
  const fetch = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({
      title: "Product status",
      projects: [
        {
          name: "API",
          state: "down",
          uptime: 75,
          history: [{ at: "2026-04-20T00:00:00Z", checks: 4, uptime: 75 }],
          incidents: [
            {
              start: "2026-04-20T10:00:00Z",
              end: null,
              cause: "Health check failed",
            },
          ],
        },
      ],
    }),
  });
  vi.stubGlobal("fetch", fetch);
  render(<PublicStatusView slug="product-status" />);
  expect(
    await screen.findByRole("heading", { name: "Product status" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent(
    "Service disruption detected",
  );
  expect(screen.getByText("75.00%")).toBeInTheDocument();
  expect(screen.getByText("Ongoing incident")).toBeInTheDocument();
  expect(
    screen.getByRole("img", { name: /75.00% uptime · 4 checks/ }),
  ).toBeInTheDocument();
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(fetch).toHaveBeenCalledWith(
    "/api/status/product-status",
    expect.objectContaining({ credentials: "omit" }),
  );
});
