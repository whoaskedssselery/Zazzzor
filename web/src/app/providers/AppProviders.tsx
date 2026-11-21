import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Fragment, type ReactNode, useEffect } from "react";
import { useLanguage } from "@/shared/i18n";
import { desktopInvoke, inDesktopApp } from "@/shared/lib";
import { UiProviders } from "@/shared/ui";

/**
 * Draws the whole interface again when the language changes: texts are read
 * with the plain `t` wherever they are, and a fresh tree reads them all in
 * the new language at once. The call, the gateway and the caches live outside
 * React, so a switch drops nothing but what is on screen. The desktop app
 * names its tray and dialogs in the same language: it is told at start and
 * on every switch (an older app without the command refuses, which is fine).
 */
const LanguageRoot = ({ children }: { children: ReactNode }) => {
  const { language } = useLanguage();
  useEffect(() => {
    if (inDesktopApp()) void desktopInvoke("set_language", { lang: language }).catch(() => undefined);
  }, [language]);
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
