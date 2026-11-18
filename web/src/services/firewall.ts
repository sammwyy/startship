import { useQuery } from "@tanstack/react-query";
import { useLiveSubscription } from "../providers/LiveProvider";
import { request, remove } from "./api";

export type Action = "block" | "allow" | "log" | "rate_limit" | "ban";
export interface Condition {
  field: string;
  op: string;
  value: string | string[];
  negate?: boolean;
}
export interface Rate {
  requests: number;
  window_seconds: number;
  by: "ip" | "ip_path";
}
export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  match: "all" | "any";
  conditions: Condition[];
  action: Action;
  rate?: Rate;
  ban_seconds?: number;
}
export interface ManagedRule {
  enabled: boolean;
  action: Action;
}
export interface Limits {
  max_body_bytes: number;
  max_url_bytes: number;
  max_header_bytes: number;
  concurrent_per_ip: number;
  rate: {
    requests: number;
    window_seconds: number;
    action: "block" | "ban";
    ban_seconds: number;
  } | null;
}
export interface Policy {
  mode: "off" | "monitor" | "enforce";
  managed: Record<string, ManagedRule>;
  limits: Limits;
  rules: Rule[] | null;
  updated: string;
  updated_by: string;
}
export interface Entry {
  value: string;
  expires_at?: string;
  note: string;
  created_by: string;
}
export interface IPList {
  id: string;
  name: string;
  kind: "block" | "allow";
  entries: Entry[] | null;
}
export interface FirewallEvent {
  time: string;
  workspace_id: string;
  project_id: string;
  route_host: string;
  ip: string;
  country: string;
  method: string;
  host: string;
  path: string;
  ua: string;
  ua_class: string;
  rule_id: string;
  rule_name: string;
  action: Action;
  status: number;
  monitored: boolean;
}
export interface Stats {
  total: number;
  blocked: number;
  by_action: Record<string, number>;
  minutes: {
    at: number;
    total: number;
    blocked: number;
    by_action: Record<string, number>;
  }[];
  top_rules: { value: string; count: number }[] | null;
  top_ips: { value: string; count: number }[] | null;
  top_countries: { value: string; count: number }[] | null;
}
export interface FirewallSnapshot {
  policy: Policy;
  lists: IPList[];
  events: FirewallEvent[];
  stats: Stats;
  can_manage: boolean;
}
export interface Catalog {
  geoip: boolean;
  fields: { id: string; ops: string[]; values?: string[] }[];
  actions: Action[];
  managed_rules: {
    id: string;
    name: string;
    description: string;
    default_action: Action;
    default_enabled: boolean;
  }[];
}
export interface NetworkRule {
  id: string;
  kind: "ip" | "cidr" | "country";
  value: string;
  ports?: "router" | "all";
  expires_at?: string;
  note: string;
}
export interface NetworkStatus {
  enabled: boolean;
  available: boolean;
  rules: NetworkRule[];
  error?: string;
}

export const firewallPath = (id: string) =>
  `workspaces/${encodeURIComponent(id)}/firewall`;
export function useFirewall(workspaceId: string, enabled = true) {
  const liveError = useLiveSubscription(
    enabled ? { topic: "firewall", workspace_id: workspaceId } : null,
  );
  const query = useQuery<FirewallSnapshot>({
    queryKey: [firewallPath(workspaceId)],
    enabled: false,
  });
  return { ...query, liveError };
}
export const useFirewallCatalog = (enabled = true) =>
  useQuery({
    queryKey: ["firewall/catalog"],
    queryFn: () => request<Catalog>("firewall/catalog"),
    enabled,
    staleTime: 60000,
  });
export const savePolicy = (id: string, policy: Policy) =>
  request<Policy>(firewallPath(id), policy, "PUT");
export const saveRule = (id: string, rule: Rule, creating: boolean) =>
  request<Policy>(
    `${firewallPath(id)}/rules${creating ? "" : `/${encodeURIComponent(rule.id)}`}`,
    rule,
    creating ? "POST" : "PUT",
  );
export const deleteRule = (id: string, rule: string) =>
  remove(`${firewallPath(id)}/rules/${encodeURIComponent(rule)}`);
export const orderRules = (id: string, ids: string[]) =>
  request(`${firewallPath(id)}/rules/order`, { ids });
export const createList = (id: string, name: string, kind: IPList["kind"]) =>
  request(`${firewallPath(id)}/lists`, { name, kind, entries: [] });
export const deleteList = (id: string, list: string) =>
  remove(`${firewallPath(id)}/lists/${encodeURIComponent(list)}`);
export const addEntry = (
  id: string,
  list: string,
  value: string,
  ttl_seconds: number,
  note: string,
) =>
  request(`${firewallPath(id)}/lists/${encodeURIComponent(list)}/entries`, {
    value,
    ttl_seconds,
    note,
  });
export const deleteEntry = (id: string, list: string, value: string) =>
  remove(
    `${firewallPath(id)}/lists/${encodeURIComponent(list)}/entries/${encodeURIComponent(value)}`,
  );
export const networkPath = "system/network-firewall";
export const useNetworkFirewall = (enabled: boolean) =>
  useQuery({
    queryKey: [networkPath],
    queryFn: () => request<NetworkStatus>(networkPath),
    enabled,
    retry: false,
  });
export const addNetworkRule = (
  rule: Pick<NetworkRule, "kind" | "value" | "ports" | "note"> & {
    ttl_seconds: number;
  },
) => request<NetworkRule>(`${networkPath}/rules`, rule);
export const deleteNetworkRule = (id: string) =>
  remove(`${networkPath}/rules/${encodeURIComponent(id)}`);

export function durationSeconds(value: string) {
  if (!value.trim() || value.trim() === "0") return 0;
  const match = /^(\d+(?:\.\d+)?)(s|m|h|d)$/.exec(value.trim());
  const seconds = match
    ? Number(match[1]) * ({ s: 1, m: 60, h: 3600, d: 86400 }[match[2]] ?? 0)
    : NaN;
  if (!Number.isInteger(seconds) || seconds < 0 || seconds > 31536000)
    throw new Error(
      "Use a whole-second duration such as 30s, 15m, 1h or 7d (up to 365d).",
    );
  return seconds;
}
export function durationText(seconds: number) {
  for (const [unit, n] of [
    ["d", 86400],
    ["h", 3600],
    ["m", 60],
    ["s", 1],
  ] as const)
    if (seconds && seconds % n === 0) return `${seconds / n}${unit}`;
  return "0";
}
