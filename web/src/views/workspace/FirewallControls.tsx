import { useEffect, useId, useState } from "react";
import { Popover } from "../../components/Popover";
import { Icon } from "../../components/Icon";
import type { Action } from "../../services/firewall";

const labels: Record<string, string> = {
  block: "Block",
  allow: "Allow",
  log: "Log",
  rate_limit: "Rate limit",
  ban: "Ban",
  ip: "IP address",
  ip_path: "IP + path",
  cidr: "CIDR",
  country: "Country",
  method: "Method",
  host: "Host",
  path: "Path",
  query: "Query",
  "header.<name>": "Header",
  user_agent: "User agent",
  ua_class: "UA class",
  device: "Device",
  scheme: "Scheme",
  eq: "Equals",
  in: "In",
  contains: "Contains",
  prefix: "Starts with",
  suffix: "Ends with",
  matches: "Matches regex",
  exists: "Exists",
  in_cidr: "In CIDR",
  in_list: "In list",
  browser: "Browser",
  crawler: "Crawler",
  bot: "Bot",
  cli: "CLI",
  library: "Library",
  unknown: "Unknown",
  empty: "Empty",
  desktop: "Desktop",
  mobile: "Mobile",
  tablet: "Tablet",
  http: "HTTP",
  https: "HTTPS",
};
export const optionLabel = (value: string) => labels[value] || value;

const durationUnits = { s: 1, m: 60, h: 3600, d: 86400 };
export function Duration({
  label,
  value,
  onChange,
  permanent = false,
}: {
  label: string;
  value: number;
  onChange: (seconds: number) => void;
  permanent?: boolean;
}) {
  const [unit, setUnit] = useState<keyof typeof durationUnits>(() =>
    value && value % 86400 === 0
      ? "d"
      : value && value % 3600 === 0
        ? "h"
        : value && value % 60 === 0
          ? "m"
          : "s",
  );
  const [finite, setFinite] = useState(value || 900);
  const [isPermanent, setPermanent] = useState(permanent && value === 0);
  const forever = permanent && isPermanent;
  return (
    <label>
      {label}
      <div className="fw-size fw-duration">
        {!forever && (
          <input
            ref={(input) => {
              input?.setCustomValidity(
                Number.isInteger(value) && value > 0
                  ? ""
                  : "Use a whole-second duration.",
              );
            }}
            aria-label={label}
            type="number"
            min={1 / durationUnits[unit]}
            max={31536000 / durationUnits[unit]}
            step="any"
            required
            value={value / durationUnits[unit]}
            onChange={(e) => {
              const n = Number(e.target.value) * durationUnits[unit];
              if (n > 0 && Number.isInteger(n)) setFinite(n);
              onChange(n);
            }}
          />
        )}
        <Picker
          label={`${label} unit`}
          value={forever ? "permanent" : unit}
          options={[
            { value: "s", label: "Seconds" },
            { value: "m", label: "Minutes" },
            { value: "h", label: "Hours" },
            { value: "d", label: "Days" },
            ...(permanent ? [{ value: "permanent", label: "Permanent" }] : []),
          ]}
          onChange={(u) => {
            if (u === "permanent") {
              setPermanent(true);
              setFinite(value || finite);
              onChange(0);
            } else {
              setPermanent(false);
              const next = u as keyof typeof durationUnits;
              onChange(
                forever
                  ? finite
                  : (value / durationUnits[unit]) * durationUnits[next],
              );
              setUnit(next);
            }
          }}
        />
      </div>
    </label>
  );
}

export function Picker({
  label,
  value,
  options,
  onChange,
  disabled = false,
}: {
  label: string;
  value: string;
  options: readonly (string | { value: string; label: string })[];
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const id = useId();
  const items = options.map((o) =>
    typeof o === "string" ? { value: o, label: optionLabel(o) } : o,
  );
  return (
    <Popover
      id={id}
      label={label}
      disabled={disabled}
      menuRole="listbox"
      className="fw-picker"
      menuClassName="fw-options"
      trigger={
        <>
          <span>
            {items.find((o) => o.value === value)?.label || value || "Choose…"}
          </span>
          <Icon name="chevron" />
        </>
      }
    >
      {items.map((o) => (
        <button
          type="button"
          key={o.value}
          role="option"
          aria-selected={o.value === value}
          onClick={() => onChange(o.value)}
        >
          {o.label}
          {o.value === value && <Icon name="check" />}
        </button>
      ))}
    </Popover>
  );
}
export function Toggle({
  label,
  checked,
  onChange,
  disabled = false,
}: {
  label: string;
  checked: boolean;
  onChange: () => void;
  disabled?: boolean;
}) {
  return (
    <input
      type="checkbox"
      className="switch"
      role="switch"
      aria-label={label}
      checked={checked}
      disabled={disabled}
      onChange={onChange}
    />
  );
}
export function ActionPill({
  action,
  monitored = false,
}: {
  action: Action;
  monitored?: boolean;
}) {
  return (
    <span className={`fw-action fw-action-${action}`}>
      {monitored ? "Monitor · " : ""}
      {optionLabel(action)}
    </span>
  );
}
export function Expiry({ value }: { value?: string }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!value) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [value]);
  if (!value) return <span className="muted">Permanent</span>;
  const seconds = Math.max(0, Math.ceil((Date.parse(value) - now) / 1000));
  return (
    <span title={new Date(value).toLocaleString()}>
      {seconds <= 0
        ? "Expired"
        : seconds >= 86400
          ? `${Math.ceil(seconds / 86400)}d left`
          : seconds >= 3600
            ? `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m left`
            : `${Math.floor(seconds / 60)}m ${seconds % 60}s left`}
    </span>
  );
}
