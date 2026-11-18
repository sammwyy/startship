let reloading = false;

export function reloadDashboard() {
  if (reloading) return;
  reloading = true;
  window.location.reload();
}

export function watchDashboardChunks(
  entrypoint: string,
  reload = reloadDashboard,
) {
  const key = `startship:chunk-reload:${entrypoint}`;
  const onError = () => {
    try {
      if (sessionStorage.getItem(key)) return;
      sessionStorage.setItem(key, "1");
    } catch {
      return;
    }
    reload();
  };
  window.addEventListener("vite:preloadError", onError);
  return () => window.removeEventListener("vite:preloadError", onError);
}
