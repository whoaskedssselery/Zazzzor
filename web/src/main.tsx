import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createQueryClient } from "./api/query-client";
import { AppProviders } from "./design/providers";
import { applyTheme, readThemePreference } from "./design/theme";
import { initSession } from "./features/auth/session";
import { createAppRouter } from "./routes/router";
import "./index.css";

// Before the first render, so the page never flashes the wrong theme.
applyTheme(readThemePreference());

const queryClient = createQueryClient();
const router = createAppRouter(queryClient);
initSession(router, queryClient);

const root = document.getElementById("root");
if (!root) throw new Error("index.html has no #root");

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <AppProviders>
        <RouterProvider router={router} />
      </AppProviders>
    </QueryClientProvider>
  </StrictMode>,
);
