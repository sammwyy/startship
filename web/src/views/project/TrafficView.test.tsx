import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Traffic } from "../../services/types";
import { TrafficContent } from "./TrafficView";
function fixture(): Traffic {
  return {
    total: {
      requests: 10,
      errors: 2,
      client_errors: 1,
      average_ms: 125,
      max_ms: 300,
    },
    containers: [
      {
        name: "api",
        cpu_percent: 12.5,
        memory_bytes: 48 * 1048576,
        limit_bytes: 512 * 1048576,
      },
    ],
    resource_status: "ok",
    sampled_at: "2026-11-17T12:00:00Z",
    interval_seconds: 5,
    recent: [
      {
        at: "2026-11-17T12:00:00Z",
        method: "GET",
        route: "api.local/<script>",
        status: 503,
        duration_ms: 300,
      },
    ],
    buckets: Array.from({ length: 60 }, (_, i) => ({
      at: 1794916500 + i * 5,
      requests: i === 59 ? 10 : 0,
      errors: i === 59 ? 2 : 0,
      average_ms: i === 59 ? 125 : 0,
      cpu_percent: i === 59 ? 12.5 : null,
      memory_bytes: i === 59 ? 48 * 1048576 : null,
    })),
  };
}
describe("traffic dashboard", () => {
  it("renders real Docker byte fields and safely displays route text", () => {
    const { container } = render(<TrafficContent data={fixture()} />);
    expect(screen.getByText("48.0 MiB / 512.0 MiB")).toBeInTheDocument();
    expect(screen.getByText("12.5% CPU")).toBeInTheDocument();
    expect(screen.getByText("20.0%")).toBeInTheDocument();
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).not.toMatch(/NaN|undefined/);
  });
  it("keeps missing resource telemetry unavailable instead of inventing zero usage", () => {
    const data = fixture();
    data.resource_status = "unavailable";
    data.containers = [];
    data.buckets.forEach((b) => {
      b.cpu_percent = null;
      b.memory_bytes = null;
    });
    const { container } = render(<TrafficContent data={data} />);
    expect(
      screen.getAllByText(/Container metrics unavailable/).length,
    ).toBeGreaterThan(0);
    expect(screen.getAllByText("Waiting for samples")).toHaveLength(2);
    expect(container.textContent).not.toMatch(/NaN|undefined|0\.0 MiB/);
  });
});
