import { useEffect, useId, useState } from "react";
import { Popover } from "../../components/Popover";
import { Icon } from "../../components/Icon";
import type { Action } from "../../services/firewall";

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
    typeof o === "string" ? { value: o, label: o.replaceAll("_", " ") } : o,
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
      {action.replaceAll("_", " ")}
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
