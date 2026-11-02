import { Tooltip } from "radix-ui";
import type { ReactNode } from "react";
import { ToastProvider } from "./components/Toast";
import { ThemeProvider } from "./theme";

/**
 * The design system's providers: theme, toasts and the shared tooltip delay.
 * app/providers puts them around the app; renderUi (shared/test) around a
 * component under test.
 */
export const UiProviders = ({ children }: { children: ReactNode }) => (
  <ThemeProvider>
    <Tooltip.Provider delayDuration={400} skipDelayDuration={200}>
      <ToastProvider>{children}</ToastProvider>
    </Tooltip.Provider>
  </ThemeProvider>
);
