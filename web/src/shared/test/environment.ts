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

// useTheme follows the OS preference for "system"; jsdom has no matchMedia.
// Tests that need a given preference replace it with their own stub.
Object.defineProperty(window, "matchMedia", {
  configurable: true,
  writable: true,
  value: (query: string): MediaQueryList => {
    const list = new EventTarget() as MediaQueryList;
    return Object.assign(list, { matches: false, media: query, onchange: null });
  },
});

export {};
