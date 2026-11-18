import { useId, useRef, useState } from "react";
import type { Certificate, WorkspaceDomain } from "../services/types";
import { request } from "../services/api";
import {
  certificateCovers,
  certificateExpiry,
  certificatesPath,
} from "../services/certificates";
import { Popover } from "./Popover";
import { Icon } from "./Icon";
import "./CertificatePicker.css";

function description(certificate: Certificate) {
  return `${certificate.source === "acme" ? "Let’s Encrypt" : "Uploaded"} · ${certificate.status === "pending" ? "Issuing" : certificate.status.charAt(0).toUpperCase() + certificate.status.slice(1)} · ${certificateExpiry(certificate)}`;
}

export function CertificatePicker({
  host,
  domains,
  admin,
  workspaceId,
  certificates,
  available,
  value,
  onChange,
}: {
  host: string;
  domains: WorkspaceDomain[];
  admin?: boolean;
  workspaceId?: string;
  certificates: Certificate[];
  available: boolean;
  value: string;
  onChange: (id: string) => void;
}) {
  const id = useId();
  const [created, setCreated] = useState<Certificate>();
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState("");
  const currentHost = useRef(host);
  currentHost.current = host;
  const all =
    created && !certificates.some((c) => c.id === created.id)
      ? [...certificates, created]
      : certificates;
  const normalized = host.trim().toLowerCase().replace(/\.$/, "");
  const matching = all.filter((c) => certificateCovers(c, normalized));
  const selected = all.find((c) => c.id === value);
  const permitted =
    admin ||
    domains.some(
      (d) =>
        (d.apex && normalized === d.name.toLowerCase()) ||
        (d.subdomains && normalized.endsWith(`.${d.name.toLowerCase()}`)),
    );
  const reason = !available
    ? "Requires workspace.domains permission and access to certificates."
    : !workspaceId
      ? "Workspace unavailable."
      : !normalized
        ? "Enter a hostname first."
        : normalized.includes("*")
          ? "HTTP-01 cannot issue wildcard certificates."
          : !/^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(
                normalized,
              )
            ? "Enter a valid domain hostname first."
            : !permitted
              ? "Hostname is not covered by workspace domain claims."
              : matching.some(
                    (c) =>
                      c.source === "acme" &&
                      ["pending", "issuing", "active"].includes(c.status),
                  )
                ? "An issuing or active Let’s Encrypt certificate already covers this hostname."
                : generating
                  ? "Certificate request in progress."
                  : "";
  async function generate() {
    if (reason || !workspaceId) return;
    const requestedHost = host;
    setGenerating(true);
    setError("");
    try {
      const certificate = await request<Certificate>(
        certificatesPath(workspaceId),
        { source: "acme", domains: [normalized], auto_renew: true },
      );
      setCreated(certificate);
      if (currentHost.current === requestedHost) onChange(certificate.id);
    } catch (error) {
      setError(
        error instanceof Error ? error.message : "Certificate request failed.",
      );
    } finally {
      setGenerating(false);
    }
  }
  return (
    <div className="certificate-picker">
      <span className="field-label">Certificate</span>
      <input type="hidden" name="certificate_id" value={value} />
      <Popover
        id={id}
        label="Certificate"
        className="certificate-picker-trigger"
        menuClassName="certificate-picker-menu"
        menuRole="listbox"
        disabled={!available}
        trigger={
          <>
            <span>
              {selected ? (
                <>
                  {selected.domains.join(", ")}
                  <small>{description(selected)}</small>
                </>
              ) : value ? (
                "Current certificate — unavailable or does not cover this host"
              ) : (
                "Automatic — any matching certificate"
              )}
            </span>
            <Icon name="chevron" />
          </>
        }
      >
        <button
          type="button"
          role="option"
          aria-selected={!value}
          onClick={() => onChange("")}
        >
          <strong>Automatic</strong>
          <small>Use any matching certificate</small>
        </button>
        {matching.map((c) => (
          <button
            key={c.id}
            type="button"
            role="option"
            aria-selected={value === c.id}
            onClick={() => onChange(c.id)}
          >
            <strong>{c.domains.join(", ")}</strong>
            <small>{description(c)}</small>
            {c.last_error && (
              <small className="certificate-picker-error">{c.last_error}</small>
            )}
          </button>
        ))}
        <button
          type="button"
          role="option"
          aria-selected={false}
          disabled={!!reason}
          onClick={() => void generate()}
        >
          <strong>
            {generating
              ? "Requesting certificate…"
              : `Generate certificate for ${normalized || "this host"}`}
          </strong>
          <small>
            {reason ||
              "Let’s Encrypt · DNS must point here and port 80 must be reachable."}
          </small>
        </button>
      </Popover>
      {!available && (
        <p className="small muted">
          Certificate selection requires workspace.domains permission and access
          to the certificate list. Existing binding is preserved.
        </p>
      )}
      {selected && (
        <p className="small muted" role="status">
          {description(selected)}
          {!certificateCovers(selected, normalized) &&
            " · Does not cover this hostname"}
        </p>
      )}
      {(error || selected?.last_error) && (
        <p className="certificate-picker-error small" role="alert">
          {error || selected?.last_error}
        </p>
      )}
    </div>
  );
}
