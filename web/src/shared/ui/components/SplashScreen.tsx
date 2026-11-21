import { t } from "@/shared/i18n";
import { cn } from "../cn";
import { Logo } from "./Logo";

interface SplashScreenProps {
  /**
   * Fading out: the first data is in. The screen stops taking clicks at
   * once and the caller unmounts it after --duration-slow.
   */
  leaving?: boolean;
}

/**
 * The loading screen at start-up, as Discord's: the animated mark in the
 * middle of the darkest surface, over everything. The caption fades in only
 * when loading takes a while. Hidden from assistive
 * technology: the connection banner beneath it announces «Подключение…»,
 * and the page under it is inert while it shows. index.html draws the same
 * screen before the bundle runs, so the hand-over is seamless.
 */
export const SplashScreen = ({ leaving = false }: SplashScreenProps) => (
  <div
    aria-hidden
    data-splash
    className={cn(
      "fixed inset-0 z-(--z-splash) grid place-items-center bg-rail",
      "transition-opacity duration-(--duration-slow) ease-(--ease-out) motion-reduce:transition-none",
      leaving && "pointer-events-none opacity-0",
    )}
  >
    {/* The caption hangs below the mark, so the mark sits where index.html's does. */}
    <div className="relative">
      <Logo size={96} animated />
      <p className="splash-caption absolute top-full left-1/2 mt-6 -translate-x-1/2 text-sm whitespace-nowrap text-fg-muted">
        {t("common:connecting")}
      </p>
    </div>
  </div>
);
