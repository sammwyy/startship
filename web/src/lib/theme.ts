import { useSyncExternalStore } from "react";

export type ThemePreference = "system" | "light" | "dark";
export const themeStyles = ["carbon", "tinted"] as const;
export const accents = [
  "green",
  "blue",
  "violet",
  "pink",
  "orange",
  "cyan",
] as const;
export type ThemeStyle = (typeof themeStyles)[number];
export type Accent = (typeof accents)[number];

function storageKey(suffix = "") {
  const prefix = window.location.pathname.startsWith("/status/")
    ? "startship-status-theme"
    : "startship-theme";
  return prefix + suffix;
}
function resolveTheme(preference: ThemePreference) {
  return preference === "system"
    ? window.matchMedia?.("(prefers-color-scheme: light)").matches
      ? "light"
      : "dark"
    : preference;
}
function preference(value?: string | null): ThemePreference {
  return value === "light" || value === "dark" ? value : "system";
}
function themeStyle(value?: string | null): ThemeStyle {
  return themeStyles.find((style) => style === value) ?? "tinted";
}
function accent(value?: string | null): Accent {
  return accents.find((color) => color === value) ?? "green";
}
function applyTheme(value: ThemePreference) {
  const root = document.documentElement;
  root.dataset.themePreference = value;
  root.dataset.theme = resolveTheme(value);
  notifyThemeChanged();
}
function notifyThemeChanged() {
  window.dispatchEvent(new Event("theme-changed"));
}
export function initTheme() {
  const root = document.documentElement;
  const media = window.matchMedia?.("(prefers-color-scheme: light)");
  media?.addEventListener("change", () => {
    if (preference(root.dataset.themePreference) === "system")
      applyTheme("system");
  });
  window.addEventListener("storage", (event) => {
    const { key, newValue } = event;
    if (key === storageKey("-style") || key === null)
      root.dataset.themeStyle = themeStyle(newValue);
    if (key === storageKey("-accent") || key === null)
      root.dataset.accent = accent(newValue);
    if (key === storageKey() || key === null) applyTheme(preference(newValue));
    else if (key === storageKey("-style") || key === storageKey("-accent"))
      notifyThemeChanged();
  });
}
export function setThemePreference(value: ThemePreference) {
  applyTheme(value);
  savePreference(storageKey(), value);
}
export function setThemeStyle(value: ThemeStyle) {
  document.documentElement.dataset.themeStyle = value;
  notifyThemeChanged();
  savePreference(storageKey("-style"), value);
}
export function setAccent(value: Accent) {
  document.documentElement.dataset.accent = value;
  notifyThemeChanged();
  savePreference(storageKey("-accent"), value);
}
function savePreference(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // The preference still applies until this page is closed.
  }
}
function subscribe(callback: () => void) {
  window.addEventListener("theme-changed", callback);
  return () => window.removeEventListener("theme-changed", callback);
}
export function useThemePreference() {
  return useSyncExternalStore(subscribe, () =>
    preference(document.documentElement.dataset.themePreference),
  );
}
export function useThemeStyle() {
  return useSyncExternalStore(subscribe, () =>
    themeStyle(document.documentElement.dataset.themeStyle),
  );
}
export function useAccent() {
  return useSyncExternalStore(subscribe, () =>
    accent(document.documentElement.dataset.accent),
  );
}
export function useAppliedTheme() {
  return useSyncExternalStore(
    subscribe,
    () => document.documentElement.dataset.theme || "dark",
  );
}
