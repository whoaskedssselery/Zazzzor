import { markdownText, SPOILER_TEXT } from "./plain";

describe("markdownText", () => {
  it("drops the marks and keeps the text", () => {
    expect(markdownText("**привет** _всем_ ~~нет~~ __да__ `код`")).toBe("привет всем нет да код");
  });

  it("keeps a link's text and literal HTML", () => {
    expect(markdownText("[тут](https://example.com) <script>x</script>")).toBe("тут <script>x</script>");
  });

  it("hides spoilers", () => {
    expect(markdownText("конец: ||он выжил||")).toBe(`конец: ${SPOILER_TEXT}`);
  });

  it("puts quotes, code blocks and line breaks on one line", () => {
    expect(markdownText("> цитата\nответ\n```\na\n  b\n```")).toBe("цитата ответ a b");
  });
});
