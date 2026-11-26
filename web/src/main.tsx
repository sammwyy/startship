import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SessionProvider } from "./providers/SessionProvider";
import { UIProvider } from "./providers/UIProvider";
import { router } from "./router";
import { initTheme } from "./lib/theme";
import { watchDashboardChunks } from "./lib/dashboardReload";
import { PublicStatusView } from "./views/PublicStatusView";
import "../style.css";
import "../traffic.css";
import "../access.css";
import "./styles.css";
import "./uptime.css";

initTheme();
watchDashboardChunks(import.meta.url);
const client = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: 1000, refetchOnWindowFocus: false },
  },
});
const statusSlug = window.location.pathname.match(
  /^\/status\/([a-z0-9-]+)$/i,
)?.[1];
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {statusSlug ? (
      <PublicStatusView slug={statusSlug} />
    ) : (
      <QueryClientProvider client={client}>
        <SessionProvider>
          <UIProvider>
            <RouterProvider router={router} />
          </UIProvider>
        </SessionProvider>
      </QueryClientProvider>
    )}
  </StrictMode>,
);
