import { useRouterState } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useConnectionStatus } from "@/entities/guild";

/** How long the loading screen waits for the gateway before it gives way to the connection banner. */
export const SPLASH_TIMEOUT_MS = 8000;
/** The fade-out, --duration-slow in tokens.css. */
const LEAVE_MS = 260;

export type SplashPhase = "showing" | "leaving" | "gone";

/**
 * The loading screen of the protected screens: it shows until the gateway is
 * ready for the first time (READY has brought the servers, the DMs and the
 * friends), then fades out. A gateway that takes longer than
 * SPLASH_TIMEOUT_MS no longer hides the app: the screen goes, and the
 * connection banner keeps saying «Подключение…». A later reconnect never
 * brings it back.
 */
export const useSplash = (): SplashPhase => {
  const firstReady = useConnectionStatus() !== "connecting";
  const [timedOut, setTimedOut] = useState(false);
  const [gone, setGone] = useState(false);
  const leaving = firstReady || timedOut;
  useEffect(() => {
    if (leaving) return;
    const t = setTimeout(() => {
      setTimedOut(true);
    }, SPLASH_TIMEOUT_MS);
    return () => {
      clearTimeout(t);
    };
  }, [leaving]);
  useEffect(() => {
    if (!leaving) return;
    const t = setTimeout(() => {
      setGone(true);
    }, LEAVE_MS);
    return () => {
      clearTimeout(t);
    };
  }, [leaving]);
  if (gone) return "gone";
  return leaving ? "leaving" : "showing";
};

/**
 * Removes index.html's boot splash once the first navigation has settled:
 * by then the page that replaces it is on screen (the sign-in form, or the
 * protected screens with their own SplashScreen), so nothing blank shows in
 * between.
 */
export const useBootSplashHandOver = (): void => {
  const loading = useRouterState({ select: (s) => s.isLoading });
  useEffect(() => {
    if (!loading) document.getElementById("boot-splash")?.remove();
  }, [loading]);
};
