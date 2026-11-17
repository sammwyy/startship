import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Traffic } from "../services/types";
import { TrafficBars } from "./TrafficCharts";

afterEach(cleanup);
describe("request chart scale", () => {
  it.each([
    [0, 2],
    [1, 2],
    [2, 2],
    [3, 4],
    [7, 8],
  ])("uses distinct integer ticks for a peak of %i", (peak, maximum) => {
    const data: Traffic = {
      total: {
        requests: peak,
        errors: 0,
        client_errors: 0,
        average_ms: 0,
        max_ms: 0,
      },
      containers: [],
      resource_status: "none",
      sampled_at: "2026-11-17T23:00:00Z",
      interval_seconds: 5,
      recent: [],
      buckets: [
        {
          at: 1,
          requests: peak,
          errors: 0,
          average_ms: 0,
          cpu_percent: null,
          memory_bytes: null,
        },
      ],
    };
    const { container } = render(<TrafficBars data={data} />);
    expect(
      Array.from(
        container.querySelectorAll(".traffic-y-axis span"),
        (el) => el.textContent,
      ),
    ).toEqual([String(maximum), String(maximum / 2), "0"]);
    expect(screen.getByRole("img").getAttribute("aria-label")).toContain(
      `peak ${peak} requests`,
    );
  });
});
