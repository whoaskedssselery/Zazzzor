import { useCallback, useSyncExternalStore } from "react";

/** Whether the CSS media query `query` matches now; re-renders when that changes. */
export function useMediaQuery(query: string): boolean {
  // Stable per query, so the store subscribes once rather than on every render.
  const subscribe = useCallback(
    (onChange: () => void) => {
      const list = window.matchMedia(query);
      list.addEventListener("change", onChange);
      return () => {
        list.removeEventListener("change", onChange);
      };
    },
    [query],
  );
  return useSyncExternalStore(subscribe, () => window.matchMedia(query).matches);
}
