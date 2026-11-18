import { useQuery } from "@tanstack/react-query";
import { useLiveSubscription } from "../providers/LiveProvider";
import { request } from "./api";
import type { Certificate } from "./types";

export const certificatesPath = (id: string) => `workspaces/${id}/certificates`;

export function useCertificates(workspaceId: string, enabled: boolean) {
  const liveError = useLiveSubscription(
    enabled ? { topic: "certificates", workspace_id: workspaceId } : null,
  );
  const query = useQuery({
    queryKey: [certificatesPath(workspaceId)],
    queryFn: () =>
      request<{ certificates: Certificate[] }>(certificatesPath(workspaceId)),
    enabled,
    retry: false,
  });
  return { ...query, liveError };
}

export function certificateCovers(certificate: Certificate, host: string) {
  const normalized = host.toLowerCase().replace(/\.$/, "");
  return certificate.domains.some((domain) => {
    domain = domain.toLowerCase().replace(/\.$/, "");
    if (!domain.startsWith("*.")) return normalized === domain;
    const suffix = domain.slice(1);
    return (
      normalized.endsWith(suffix) &&
      !!normalized.slice(0, -suffix.length) &&
      !normalized.slice(0, -suffix.length).includes(".")
    );
  });
}

export function certificateUsable(certificate: Certificate, now = Date.now()) {
  return (
    !!certificate.cert_pem &&
    ["active", "issuing", "failed"].includes(certificate.status) &&
    Date.parse(certificate.not_before) <= now &&
    now < Date.parse(certificate.not_after)
  );
}

export function certificateExpiry(certificate: Certificate, now = Date.now()) {
  const expiry = Date.parse(certificate.not_after);
  if (!Number.isFinite(expiry) || expiry <= 0) return "Not issued";
  const days = Math.ceil((expiry - now) / 86400000);
  return `${new Date(expiry).toLocaleDateString()} · ${days > 0 ? `${days} days left` : "Expired"}`;
}
