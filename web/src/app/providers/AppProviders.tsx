import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { UiProviders } from "@/shared/ui";

/** Everything the app runs inside: the query cache, then the design system's theme, tooltips and toasts. */
export function AppProviders({ queryClient, children }: { queryClient: QueryClient; children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <UiProviders>{children}</UiProviders>
    </QueryClientProvider>
  );
}
