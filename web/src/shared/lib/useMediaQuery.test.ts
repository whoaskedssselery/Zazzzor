import { renderHook } from "@testing-library/react";
import { useMediaQuery } from "./useMediaQuery";

describe("useMediaQuery", () => {
  it("subscribes to the query once, not on every render", () => {
    let subscriptions = 0;
    const real = Object.getOwnPropertyDescriptor(window, "matchMedia");
    const make = window.matchMedia.bind(window);
    window.matchMedia = (query: string) => {
      const list = make(query);
      const add = list.addEventListener.bind(list);
      list.addEventListener = (...args: Parameters<typeof add>) => {
        subscriptions += 1;
        add(...args);
      };
      return list;
    };
    onTestFinished(() => {
      if (real) Object.defineProperty(window, "matchMedia", real);
    });

    const { rerender } = renderHook(() => useMediaQuery("(min-width: 768px)"));
    rerender();
    rerender();
    rerender();
    expect(subscriptions).toBe(1);
  });
});
