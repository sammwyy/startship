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
}: {
  domains: WorkspaceDomain[];
  host?: string;
  admin?: boolean;
}) {
  const exact = domains.find((d) => d.apex && d.name === host),
    wildcard =
      !exact &&
      domains.find((d) => d.subdomains && host.endsWith(`.${d.name}`));
  const [selected, setSelected] = useState(
    exact?.name ||
      (wildcard
        ? `*.${wildcard.name}`
        : host && admin
          ? ""
          : domains.length
            ? `${domains[0].subdomains ? "*." : ""}${domains[0].name}`
            : ""),
  );
  if (!domains.length && !admin)
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
          defaultValue={
            wildcard ? host.slice(0, -wildcard.name.length - 1) : ""
          }
          required={mode === "sub"}
          autoComplete="off"
        />
        <span className="host-dot">.</span>
        <select
          name="domain"
          aria-label="Domain"
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
        >
          {domains.flatMap(domainPatterns).map((p) => (
            <option key={p}>{p}</option>
          ))}
          {admin && <option value="">Any hostname (Platform Admin)</option>}
        </select>
        <input
          name="host"
          aria-label="Hostname"
          placeholder="example.com"
          defaultValue={!exact && !wildcard ? host : ""}
          required={mode === "custom"}
          autoComplete="off"
        />
      </div>
    </div>
  );
}
