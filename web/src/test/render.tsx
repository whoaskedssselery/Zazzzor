import { render, type RenderOptions } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import { AppProviders } from "../design/providers";

/**
 * Renders `ui` inside the app-wide providers (theme, toasts, tooltips), as
 * main.tsx does, and returns a user-event instance bound to the document.
 */
export function renderUi(ui: ReactElement, options?: Omit<RenderOptions, "wrapper">) {
  const user = userEvent.setup();
  return { user, ...render(ui, { wrapper: AppProviders, ...options }) };
}
