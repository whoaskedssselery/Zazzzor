import { Tooltip } from "radix-ui";
import type { ReactNode } from "react";
import { ToastProvider } from "./components/Toast";
import { ThemeProvider } from "./theme";

/** App-wide UI providers: theme, toasts and the shared tooltip delay. */
export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <ThemeProvider>
      <Tooltip.Provider delayDuration={400} skipDelayDuration={200}>
        <ToastProvider>{children}</ToastProvider>
      </Tooltip.Provider>
    </ThemeProvider>
  );
}
