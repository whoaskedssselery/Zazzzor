/**
 * A layout for virtualised lists, which jsdom cannot give: it lays nothing
 * out, so a list's viewport is 0 px tall and no row ever renders. The
 * element marked `data-virtual-scroller` gets a viewport of `viewport` px,
 * each row (`data-index`, as TanStack Virtual marks them) the height
 * `row(el)` gives, and the scroller's content height is its list's own
 * (the first <ol>'s style height plus `below` px under it).
 *
 * jsdom also fires no scroll event after a programmatic scroll; a test that
 * reads the rendered rows after one fires it itself.
 *
 * Returns the function that puts jsdom back; call it after the test.
 */
export function fakeListLayout({
  viewport = 600,
  row = () => 40,
  below = 16,
}: {
  viewport?: number;
  row?: (el: HTMLElement) => number;
  below?: number;
} = {}): () => void {
  const isScroller = (el: Element) => el.hasAttribute("data-virtual-scroller");
  const isRow = (el: Element) => el.hasAttribute("data-index");
  const rect = (height: number) =>
    ({ x: 0, y: 0, top: 0, left: 0, right: 800, bottom: height, width: 800, height, toJSON: () => ({}) }) as DOMRect;
  const saved = (["offsetHeight", "clientHeight", "scrollHeight"] as const).map(
    (name) => [name, Object.getOwnPropertyDescriptor(HTMLElement.prototype, name)] as const,
  );
  const rectSpy = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (
    this: HTMLElement,
  ) {
    if (isScroller(this)) return rect(viewport);
    return rect(isRow(this) ? row(this) : 0);
  });
  const define = (name: string, get: (el: HTMLElement) => number) => {
    Object.defineProperty(HTMLElement.prototype, name, {
      configurable: true,
      get(this: HTMLElement) {
        return get(this);
      },
    });
  };
  // The virtualiser measures a row by its offsetHeight.
  define("offsetHeight", (el) => (isRow(el) ? row(el) : 0));
  define("clientHeight", (el) => (isScroller(el) ? viewport : 0));
  define("scrollHeight", (el) =>
    isScroller(el) ? Number.parseFloat(el.querySelector("ol")?.style.height ?? "0") + below : 0,
  );
  return () => {
    rectSpy.mockRestore();
    for (const [name, descriptor] of saved) {
      if (descriptor) Object.defineProperty(HTMLElement.prototype, name, descriptor);
      else Reflect.deleteProperty(HTMLElement.prototype, name);
    }
  };
}
