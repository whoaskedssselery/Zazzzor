import type { ReactNode } from "react";
import { LogoLockup } from "@/shared/ui";

/**
 * The frame of the sign-in and registration screens: one card in the middle
 * of the darkest surface, the way Discord greets you, in the app's own warm
 * tones, under the mark and the name. The card is the only raised thing on
 * the page.
 */
export const AuthLayout = ({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) => (
  <main className="flex min-h-dvh flex-col items-center justify-center bg-rail px-4 py-10">
    <p className="mb-6">
      <LogoLockup fontSize={28} />
    </p>
    <section
      aria-labelledby="auth-title"
      className="w-full max-w-[30rem] rounded-lg bg-chat p-8 shadow-dialog max-sm:px-5 max-sm:py-6"
    >
      <header className="mb-6 text-center">
        <h1 id="auth-title" className="text-xl font-semibold text-balance text-fg">
          {title}
        </h1>
        <p className="mt-2 text-base text-fg-muted">{subtitle}</p>
      </header>
      {children}
    </section>
  </main>
);
