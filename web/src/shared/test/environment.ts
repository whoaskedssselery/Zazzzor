// Browser behaviour jsdom lacks, patched before anything else is loaded:
// setup.ts imports this module first, ahead of the MSW server and through it
// the API client, which reads Request when it is created.

// jsdom keeps Node's fetch, whose Request rejects relative URLs; a browser
// resolves them against the page. Do the same, so `api` keeps its real
// same-origin baseUrl "/api/v1" in tests (it reads Request when created).
const NodeRequest = globalThis.Request;
globalThis.Request = class extends NodeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === "string" ? new URL(input, window.location.href) : input, init);
  }
};

// jsdom has no layout; the router resets the scroll position on navigation.
window.scrollTo = () => {};

// Browser APIs jsdom lacks that Radix primitives call: popper content measures
// itself with ResizeObserver, Select scrolls the selected item into view and
// captures the pointer. No-ops are enough without layout.
class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
Object.assign(globalThis, { ResizeObserver: NoopResizeObserver });
Object.assign(Element.prototype, {
  scrollIntoView: () => {},
  hasPointerCapture: () => false,
  releasePointerCapture: () => {},
});

// jsdom has no matchMedia. Width queries ("(min-width: 1100px)",
// "(max-width: …)") are answered from window.innerWidth, jsdom's 1024 unless a
// test sets another before rendering; "(pointer: coarse)" matches after
// setTouchScreen(); everything else (the OS colour scheme
// useTheme follows) does not match. Tests that need a given preference
// replace it with their own stub.
const matchesWidth = (query: string): boolean => {
  if (query.includes("pointer: coarse")) return document.documentElement.dataset.testTouch === "true";
  const width = window.innerWidth;
  const min = /\(min-width:\s*(\d+)px\)/.exec(query);
  const max = /\(max-width:\s*(\d+)px\)/.exec(query);
  if (!min && !max) return false;
  return (!min || width >= Number(min[1])) && (!max || width <= Number(max[1]));
};
Object.defineProperty(window, "matchMedia", {
  configurable: true,
  writable: true,
  value: (query: string): MediaQueryList => {
    const list = new EventTarget() as MediaQueryList;
    return Object.assign(list, { matches: matchesWidth(query), media: query, onchange: null });
  },
});

export {};
