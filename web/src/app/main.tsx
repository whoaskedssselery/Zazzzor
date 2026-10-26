import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { initSession } from "@/entities/session";
import { createQueryClient } from "@/shared/api";
import { applyTheme, readThemePreference } from "@/shared/ui";
import { AppProviders } from "./providers";
import { createAppRouter } from "./router";
import "./styles/index.css";

// Before the first render, so the page never flashes the wrong theme.
applyTheme(readThemePreference());

const queryClient = createQueryClient();
const router = createAppRouter(queryClient);
initSession(router, queryClient);

const root = document.getElementById("root");
if (!root) throw new Error("index.html has no #root");

createRoot(root).render(
  <StrictMode>
    <AppProviders queryClient={queryClient}>
      <RouterProvider router={router} />
    </AppProviders>
  </StrictMode>,
);
