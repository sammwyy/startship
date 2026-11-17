import type { Traffic, TrafficBucket } from "../services/types";
export const trafficNumber = (value: number) =>
  new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
export const trafficMS = (value: number) =>
  value < 10 ? value.toFixed(1) : trafficNumber(value);
export const trafficMemory = (value: number) =>
  value >= 1073741824
    ? `${(value / 1073741824).toFixed(2)} GiB`
    : `${(value / 1048576).toFixed(1)} MiB`;
export const trafficTime = (value: string | number) =>
  new Date(value).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
function Axis({ buckets }: { buckets: TrafficBucket[] }) {
  return (
    <div className="traffic-axis">
      <span>{buckets.length ? trafficTime(buckets[0].at * 1000) : "—"}</span>
      <span>
        {buckets.length
          ? trafficTime(buckets[Math.floor(buckets.length / 2)].at * 1000)
          : "—"}
      </span>
      <span>Now</span>
    </div>
  );
}
export function TrafficBars({ data }: { data: Traffic }) {
  const peak = Math.max(0, ...data.buckets.map((b) => b.requests));
  const max = Math.max(2, Math.ceil(peak / 2) * 2);
  return (
    <>
      <div className="traffic-volume-chart">
        <div className="traffic-y-axis">
          <span>{trafficNumber(max)}</span>
          <span>{trafficNumber(max / 2)}</span>
          <span>0</span>
        </div>
        <svg
          role="img"
          aria-label={`Requests and server errors over time, peak ${peak} requests per interval`}
          viewBox={`0 0 ${Math.max(1, data.buckets.length) * 10 + 4} 164`}
          preserveAspectRatio="none"
        >
          <path className="traffic-gridline" d="M0 10H604M0 85H604M0 160H604" />
          {data.buckets.map((b, i) => {
            const height = (b.requests / max) * 150,
              errors = (b.errors / max) * 150;
            return (
              <g key={b.at}>
                <title>
                  {trafficTime(b.at * 1000)} · {b.requests} requests ·{" "}
                  {b.errors} server errors
                </title>
                <rect
                  className="traffic-bar-track"
                  x={4 + i * 10}
                  y={10}
                  width={6}
                  height={150}
                  rx={2}
                />
                <rect
                  className="traffic-bar"
                  x={4 + i * 10}
                  y={160 - height}
                  width={6}
                  height={height}
                  rx={1}
                />
                <rect
                  className="traffic-error-bar"
                  x={4 + i * 10}
                  y={160 - errors}
                  width={6}
                  height={errors}
                  rx={1}
                />
              </g>
            );
          })}
        </svg>
      </div>
      <Axis buckets={data.buckets} />
    </>
  );
}
export function TrafficLine({
  data,
  metric,
  label,
  kind,
}: {
  data: Traffic;
  metric: "average_ms" | "cpu_percent" | "memory_bytes";
  label: string;
  kind: string;
}) {
  const values = data.buckets.map((b) =>
    metric === "average_ms" && !b.requests ? null : b[metric],
  );
  const present = values.filter((v): v is number => v !== null),
    max = Math.max(kind === "memory" ? 1048576 : 1, ...present);
  let path = "";
  const points = values.map((value, i) => {
    if (value === null) return null;
    const x = 4 + i * 5,
      y = 95 - (value / max) * 80;
    path += `${i === 0 || values[i - 1] === null ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
    return (
      <circle key={data.buckets[i].at} cx={x} cy={y} r={1.8}>
        <title>
          {trafficTime(data.buckets[i].at * 1000)} ·{" "}
          {kind === "memory"
            ? trafficMemory(value)
            : `${value.toFixed(1)}${kind === "cpu" ? "%" : " ms"}`}
        </title>
      </circle>
    );
  });
  return (
    <>
      <div className={`traffic-line-chart ${kind}`}>
        <svg
          role="img"
          aria-label={`${label}${present.length ? "" : " — no samples yet"}`}
          viewBox={`0 0 ${Math.max(1, data.buckets.length) * 5 + 4} 108`}
          preserveAspectRatio="none"
        >
          <path className="traffic-gridline" d="M0 15H304M0 55H304M0 95H304" />
          <path className="traffic-line" d={path} />
          {points}
        </svg>
        {!present.length && (
          <span className="traffic-no-samples">
            {kind === "latency" ? "No requests yet" : "Waiting for samples"}
          </span>
        )}
      </div>
      <Axis buckets={data.buckets} />
    </>
  );
}
