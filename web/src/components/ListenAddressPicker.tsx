import { useId, useState } from "react";
import type { NetworkInterface } from "../services/types";
import { Icon } from "./Icon";
import { Popover } from "./Popover";

type ParsedListen =
  { mode: "interface"; iface: string; port: string } | { mode: "custom" };

const ipv4Pattern = /^\d+\.\d+\.\d+\.\d+$/;

function parseListenValue(raw: string): ParsedListen {
  const entries = raw
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean);
  if (entries.length !== 1) return { mode: "custom" };
  const entry = entries[0];
  if (entry.startsWith(":")) {
    return { mode: "interface", iface: "any", port: entry.slice(1) };
  }
  if (entry.startsWith("[")) {
    return { mode: "custom" };
  }
  const sep = entry.lastIndexOf(":");
  if (sep <= 0) return { mode: "custom" };
  const host = entry.slice(0, sep);
  const port = entry.slice(sep + 1);
  if (host === "0.0.0.0" || host === "::" || ipv4Pattern.test(host)) {
    return { mode: "custom" };
  }
  return { mode: "interface", iface: host, port };
}

function interfaceSummary(iface: NetworkInterface) {
  const addrs = iface.addresses.slice(0, 2).join(", ");
  const extra =
    iface.addresses.length > 2 ? ` +${iface.addresses.length - 2}` : "";
  const tags = [
    iface.loopback ? "Loopback" : "",
    !iface.up ? "Down" : "",
  ].filter(Boolean);
  return [addrs + extra, ...tags].filter(Boolean).join(" · ");
}

function formatListen(iface: string, port: string) {
  const trimmed = port.trim();
  return iface === "any" ? `:${trimmed}` : `${iface}:${trimmed}`;
}

export function ListenAddressPicker({
  id,
  label,
  defaultPort,
  value,
  interfaces,
  readOnly,
  disabled,
  describedBy,
  onChange,
}: {
  id: string;
  label: string;
  defaultPort: string;
  value: string;
  interfaces: NetworkInterface[];
  readOnly?: boolean;
  disabled?: boolean;
  describedBy?: string;
  onChange: (next: string) => void;
}) {
  const menuId = useId();
  const parsed = parseListenValue(value);
  const [forceCustom, setForceCustom] = useState(false);
  const customMode = forceCustom || parsed.mode === "custom";
  const currentIface = parsed.mode === "interface" ? parsed.iface : "";
  const currentPort = parsed.mode === "interface" ? parsed.port : defaultPort;
  const matchedIface = interfaces.find((item) => item.name === currentIface);
  const locked = !!disabled || !!readOnly;

  const triggerTitle = customMode
    ? "Custom addresses"
    : currentIface === "any"
      ? "Any interface"
      : currentIface;

  const triggerSubtext = customMode
    ? value.trim() || "Multiple interfaces, IPv4, or IPv6"
    : currentIface === "any"
      ? "All interfaces · 0.0.0.0 / ::"
      : matchedIface
        ? interfaceSummary(matchedIface)
        : "Configured interface (not currently detected)";

  function selectInterface(iface: string) {
    setForceCustom(false);
    const port =
      parsed.mode === "interface" && parsed.port.trim()
        ? parsed.port.trim()
        : defaultPort;
    onChange(formatListen(iface, port));
  }

  return (
    <div className="listen-picker">
      <span className="field-label">{label}</span>
      <div
        className={`listen-picker-row${customMode ? " listen-picker-custom" : ""}`}
      >
        <Popover
          id={menuId}
          label={`${label} interface`}
          className="listen-picker-trigger"
          menuClassName="listen-picker-menu"
          menuRole="listbox"
          disabled={locked}
          trigger={
            <>
              <span>
                {triggerTitle}
                <small>{triggerSubtext}</small>
              </span>
              <Icon name="chevron" />
            </>
          }
        >
          <button
            type="button"
            role="option"
            aria-selected={!customMode && currentIface === "any"}
            onClick={() => selectInterface("any")}
          >
            Any interface
            <small>All interfaces · 0.0.0.0 / ::</small>
          </button>
          {interfaces.map((iface) => (
            <button
              key={iface.name}
              type="button"
              role="option"
              aria-selected={!customMode && currentIface === iface.name}
              onClick={() => selectInterface(iface.name)}
            >
              {iface.name}
              <small>{interfaceSummary(iface)}</small>
            </button>
          ))}
          <button
            type="button"
            role="option"
            className="listen-picker-custom-option"
            aria-selected={customMode}
            onClick={() => setForceCustom(true)}
          >
            Custom addresses
            <small>Multiple interfaces, IPv4, or IPv6</small>
          </button>
        </Popover>
        {!customMode && (
          <div className="listen-picker-port">
            <input
              id={`${id}-port`}
              aria-label={`${label} port`}
              inputMode="numeric"
              placeholder={defaultPort}
              value={currentPort}
              readOnly={readOnly}
              disabled={disabled}
              aria-describedby={describedBy}
              spellCheck={false}
              onChange={(event) =>
                onChange(formatListen(currentIface, event.target.value))
              }
            />
          </div>
        )}
      </div>
      {customMode && (
        <input
          id={id}
          className="listen-picker-input"
          aria-label={label}
          value={value}
          placeholder={`tailscale0:${defaultPort}, 127.0.0.1:${defaultPort}`}
          readOnly={readOnly}
          disabled={disabled}
          aria-describedby={describedBy}
          spellCheck={false}
          onChange={(event) => onChange(event.target.value)}
        />
      )}
    </div>
  );
}
