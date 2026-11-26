import { useSyncExternalStore } from "react";

export type ThemePreference = "system" | "light" | "dark";
const storageKey = window.location.pathname.startsWith("/status/")
  ? "startship-status-theme"
  : "startship-theme";
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
function applyTheme(value: ThemePreference) {
  const root = document.documentElement;
  root.dataset.themePreference = value;
  root.dataset.theme = resolveTheme(value);
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
    if (event.key === storageKey || event.key === null)
      applyTheme(preference(event.newValue));
  });
}
export function setThemePreference(value: ThemePreference) {
  applyTheme(value);
  try {
    localStorage.setItem(storageKey, value);
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
export function useAppliedTheme() {
  return useSyncExternalStore(
    subscribe,
    () => document.documentElement.dataset.theme || "dark",
  );
}
