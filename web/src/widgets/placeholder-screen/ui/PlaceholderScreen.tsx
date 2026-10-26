import type { ReactNode } from "react";
import { LogoutButton } from "@/features/auth";

/**
 * A protected screen that is not built yet: its name, a line about it and the
 * way out. The app shell (Task 5) and the settings and admin screens (Task 8)
 * replace the pages that use it.
 */
export const PlaceholderScreen = ({ title, children }: { title: string; children: ReactNode }) => (
  <main className="grid min-h-dvh place-items-center p-6 text-center">
    <div className="max-w-md">
      <h1 className="text-xl font-semibold">{title}</h1>
      <p className="mt-2 text-fg-muted">{children}</p>
      <LogoutButton className="mt-6" />
    </div>
  </main>
);
