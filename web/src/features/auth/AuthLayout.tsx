import { CircleAlert } from "lucide-react";
import type { ReactNode } from "react";

/**
 * The frame of the sign-in and registration screens: one card in the middle
 * of the darkest surface, the way Discord greets you, in the app's own warm
 * tones. The card is the only raised thing on the page.
 */
export function AuthLayout({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <main className="flex min-h-dvh flex-col items-center justify-center bg-rail px-4 py-10">
      <p className="mb-6 text-lg font-bold tracking-tight text-fg" translate="no">
        Zazzzor
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
}

/** The form-level error under the heading: what went wrong and, when there is one, the way out. */
export function FormAlert({ children }: { children: ReactNode }) {
  return (
    <div role="alert" className="mb-5 flex gap-2.5 rounded-md bg-danger/12 px-3 py-2.5 text-sm text-fg">
      <CircleAlert aria-hidden className="mt-px size-4.5 shrink-0 text-fg-danger" strokeWidth={2} />
      <p className="min-w-0">{children}</p>
    </div>
  );
}
