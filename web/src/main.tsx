import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SessionProvider } from "./providers/SessionProvider";
import { UIProvider } from "./providers/UIProvider";
import { router } from "./router";
import { initTheme } from "./lib/theme";
import "../style.css";
import "../traffic.css";
import "../access.css";
import "./styles.css";

initTheme();
const client = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: 1000, refetchOnWindowFocus: false },
  },
});
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={client}>
      <SessionProvider>
        <UIProvider>
          <RouterProvider router={router} />
        </UIProvider>
      </SessionProvider>
    </QueryClientProvider>
  </StrictMode>,
);
