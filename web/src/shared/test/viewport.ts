/**
 * Pretends the window is `width` px wide for the rest of the test: the
 * matchMedia of the test environment answers width queries from
 * window.innerWidth. Call it before rendering; it is undone when the test ends.
 */
export function setViewportWidth(width: number): void {
  const before = window.innerWidth;
  window.innerWidth = width;
  onTestFinished(() => {
    window.innerWidth = before;
  });
}

/** Pretends the screen is a touch screen ("(pointer: coarse)" matches) for the rest of the test. */
export function setTouchScreen(): void {
  document.documentElement.dataset.testTouch = "true";
  onTestFinished(() => {
    delete document.documentElement.dataset.testTouch;
  });
}
