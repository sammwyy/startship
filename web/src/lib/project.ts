import type { Container, Release } from "../services/types";

export const projectSections = [
  ["overview", "Overview", "grid"],
  ["deployments", "Deployments", "activity"],
  ["logs", "Logs", "terminal"],
  ["containers", "Containers", "box"],
  ["connections", "Connections", "plug"],
  ["volumes", "Volumes", "hard-drive"],
  ["traffic", "Traffic", "activity"],
  ["domains", "Domains", "globe"],
  ["source", "Source", "branch"],
  ["pipeline", "Pipeline", "pipeline"],
  ["environment", "Environment", "lock"],
] as const;
export const busyRelease = (release?: Release) =>
  !!release &&
  ["queued", "building", "testing", "starting"].includes(release.status);
export const sortReleases = (releases: Release[]) =>
  [...releases].sort((a, b) => Date.parse(b.created) - Date.parse(a.created));
export function projectSummary(
  containers: Container[],
  routes: unknown[],
  sleeping = false,
) {
  const status = sleeping
    ? "sleeping"
    : `${containers.filter((c) => c.status === "running").length} running`;
  return `${containers.length} container${containers.length === 1 ? "" : "s"} · ${status} · ${routes.length} domain route${routes.length === 1 ? "" : "s"}`;
}
export function environmentData(value: string) {
  const env: Record<string, string> = Object.create(null);
  for (const line of value.split("\n")) {
    if (!line.trim() || line.trim().startsWith("#")) continue;
    const i = line.indexOf("=");
    if (i < 1 || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(line.slice(0, i).trim()))
      throw new Error("Use KEY=VALUE, one variable per line.");
    env[line.slice(0, i).trim()] = line.slice(i + 1);
  }
  return env;
}
export const text = (form: FormData, name: string) =>
  String(form.get(name) ?? "");
export function sourceData(form: FormData) {
  return {
    git_url: text(form, "git_url"),
    branch: text(form, "branch"),
    ...(form.get("git_token") ? { git_token: text(form, "git_token") } : {}),
  };
}
