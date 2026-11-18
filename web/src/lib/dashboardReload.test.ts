import { expect, it, vi } from "vitest";
import { watchDashboardChunks } from "./dashboardReload";

it("reloads only once for a failed build, including after the reload", () => {
  const reload = vi.fn();
  const fail = () => window.dispatchEvent(new Event("vite:preloadError"));
  const stop = watchDashboardChunks("entry-old.js", reload);
  fail();
  fail();
  stop();
  const retry = watchDashboardChunks("entry-old.js", reload);
  fail();
  retry();
  expect(reload).toHaveBeenCalledOnce();
  const next = watchDashboardChunks("entry-new.js", reload);
  fail();
  next();
  expect(reload).toHaveBeenCalledTimes(2);
});
