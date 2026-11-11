import { faviconFor, titleFor } from "./badge";

describe("unread badge", () => {
  it("puts unread mentions in the title", () => {
    expect(titleFor(0)).toBe("Zazzzor");
    expect(titleFor(3)).toBe("(3) Zazzzor");
  });

  it("marks the favicon red for mentions, white for unread, not at all otherwise", () => {
    const svg = (s: string) => decodeURIComponent(s.replace("data:image/svg+xml,", ""));
    expect(svg(faviconFor("none"))).not.toContain("circle");
    expect(svg(faviconFor("mentions"))).toContain("#e5484d");
    expect(svg(faviconFor("unread"))).toContain("#f5f1ea");
  });
});
