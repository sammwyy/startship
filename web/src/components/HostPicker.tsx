import { useState } from "react";
import type { WorkspaceDomain } from "../services/types";

export const domainPatterns = (domain: WorkspaceDomain) =>
  [
    domain.apex ? domain.name : "",
    domain.subdomains ? `*.${domain.name}` : "",
  ].filter(Boolean);
export function pickedHost(form: FormData) {
  const domain = String(form.get("domain") ?? "");
  return domain === ""
    ? String(form.get("host") || "")
    : domain.startsWith("*.")
      ? `${String(form.get("sub") || "")
          .trim()
          .replace(/\.$/, "")}.${domain.slice(2)}`
      : domain;
}
export function HostPicker({
  domains,
  host = "",
  admin,
  onHostChange,
}: {
  domains: WorkspaceDomain[];
  host?: string;
  admin?: boolean;
  onHostChange?: (host: string) => void;
}) {
  const exact = domains.find((d) => d.apex && d.name === host),
    wildcard =
      !exact &&
      domains.find((d) => d.subdomains && host.endsWith(`.${d.name}`));
  const existingOnly = !!host && !exact && !wildcard && !admin;
  const [selected, setSelected] = useState(
    exact?.name ||
      (wildcard
        ? `*.${wildcard.name}`
        : existingOnly
          ? host
          : host && admin
            ? ""
            : domains.length
              ? domainPatterns(domains[0])[0]
              : ""),
  );
  const [sub, setSub] = useState(
    wildcard ? host.slice(0, -wildcard.name.length - 1) : "",
  );
  const [custom, setCustom] = useState(!exact && !wildcard ? host : "");
  function change(domain: string, prefix = sub, hostname = custom) {
    setSelected(domain);
    onHostChange?.(
      domain === ""
        ? hostname
        : domain.startsWith("*.")
          ? `${prefix.trim().replace(/\.$/, "")}.${domain.slice(2)}`
          : domain,
    );
  }
  if (!domains.length && !admin && !existingOnly)
    return (
      <p className="note">
        This workspace has no domains yet. A workspace administrator adds them
        in Workspace settings.
      </p>
    );
  const mode =
    selected === "" ? "custom" : selected.startsWith("*.") ? "sub" : "exact";
  return (
    <div className="host-picker">
      <span className="field-label">Hostname</span>
      <div className="host-composer" data-mode={mode}>
        <input
          name="sub"
          aria-label="Subdomain"
          placeholder="app"
          value={sub}
          onChange={(e) => {
            setSub(e.target.value);
            change(selected, e.target.value);
          }}
          disabled={mode !== "sub"}
          required={mode === "sub"}
          autoComplete="off"
        />
        <span className="host-dot">.</span>
        <select
          name="domain"
          aria-label="Domain"
          value={selected}
          onChange={(e) => change(e.target.value)}
        >
          {existingOnly && (
            <option value={host}>{host} (existing route)</option>
          )}
          {domains.flatMap(domainPatterns).map((p) => (
            <option key={p}>{p}</option>
          ))}
          {admin && <option value="">Any hostname (Platform Admin)</option>}
        </select>
        <input
          name="host"
          aria-label="Hostname"
          placeholder="example.com"
          value={custom}
          onChange={(e) => {
            setCustom(e.target.value);
            change(selected, sub, e.target.value);
          }}
          disabled={mode !== "custom"}
          required={mode === "custom"}
          autoComplete="off"
        />
      </div>
      {existingOnly && selected === host && (
        <p className="small muted">
          This hostname is outside the workspace’s registered domains. You can
          keep it on this existing route, or choose a registered domain.
        </p>
      )}
    </div>
  );
}
