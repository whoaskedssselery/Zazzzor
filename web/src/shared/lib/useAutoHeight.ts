import { type RefObject, useEffect, useLayoutEffect } from "react";

/** Sets a text area's height to its text, up to `max` px; past that it scrolls. */
const fitToText = (el: HTMLTextAreaElement, max: number): void => {
  el.style.height = "auto";
  el.style.height = `${String(Math.min(el.scrollHeight, max))}px`;
};

/**
 * Keeps a text area as tall as its text (up to `max` px): again whenever
 * `value` changes, and whenever the area gets wider or narrower (a resized
 * window, a phone turned), since its text then wraps onto more or fewer
 * lines.
 */
export function useAutoHeight(ref: RefObject<HTMLTextAreaElement | null>, value: string, max: number): void {
  useLayoutEffect(() => {
    if (ref.current) fitToText(ref.current, max);
  }, [ref, value, max]);

  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let width = el.clientWidth;
    // Only a change of width: fitting changes the height, which must not loop.
    const observer = new ResizeObserver(() => {
      if (el.clientWidth === width) return;
      width = el.clientWidth;
      fitToText(el, max);
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
    };
  }, [ref, max]);
}
