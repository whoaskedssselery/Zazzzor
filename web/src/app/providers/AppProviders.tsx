import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Fragment, type ReactNode } from "react";
import { useLanguage } from "@/shared/i18n";
import { UiProviders } from "@/shared/ui";

/**
 * Draws the whole interface again when the language changes: texts are read
 * with the plain `t` wherever they are, and a fresh tree reads them all in
 * the new language at once. The call, the gateway and the caches live outside
 * React, so a switch drops nothing but what is on screen.
 */
const LanguageRoot = ({ children }: { children: ReactNode }) => {
  const { language } = useLanguage();
  return <Fragment key={language}>{children}</Fragment>;
};

/** Everything the app runs inside: the query cache, then the design system's theme, tooltips and toasts. */
export const AppProviders = ({ queryClient, children }: { queryClient: QueryClient; children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>
    <UiProviders>
      <LanguageRoot>{children}</LanguageRoot>
    </UiProviders>
  </QueryClientProvider>
);
