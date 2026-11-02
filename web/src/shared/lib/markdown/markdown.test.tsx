import { screen } from "@testing-library/react";
import { renderUi } from "@/shared/test";
import { MAX_DEPTH, type MdNode, parseMarkdown } from "./parse";
import { Markdown } from "./render";

const text = (value: string): MdNode => ({ type: "text", value });
const cp = (...codes: number[]) => String.fromCodePoint(...codes);

describe("parseMarkdown", () => {
  it.each([
    ["**жирный**", "bold"],
    ["*курсив*", "italic"],
    ["_курсив_", "italic"],
    ["__подчёркнутый__", "underline"],
    ["~~зачёркнутый~~", "strike"],
    ["||спойлер||", "spoiler"],
  ] as const)("reads %s as %s", (src, type) => {
    const inner = src.replace(/^[*_~|]+|[*_~|]+$/g, "");
    expect(parseMarkdown(src)).toEqual([{ type, children: [text(inner)] }]);
  });

  it("nests inline styles", () => {
    expect(parseMarkdown("**жирный *и курсив***")).toEqual([
      { type: "bold", children: [text("жирный "), { type: "italic", children: [text("и курсив")] }] },
    ]);
  });

  it("keeps snake_case words as they are", () => {
    expect(parseMarkdown("some_snake_case_name")).toEqual([text("some_snake_case_name")]);
  });

  it("reads inline code without markdown inside", () => {
    expect(parseMarkdown("до `**не жирный**` после")).toEqual([
      text("до "),
      { type: "code", value: "**не жирный**" },
      text(" после"),
    ]);
  });

  it("reads a code block with and without a language", () => {
    expect(parseMarkdown("```go\nfmt.Println(1)\n```")).toEqual([
      { type: "codeBlock", lang: "go", value: "fmt.Println(1)" },
    ]);
    expect(parseMarkdown("```**x**```")).toEqual([{ type: "codeBlock", lang: null, value: "**x**" }]);
  });

  it("drops the line breaks around a code block, which is a block itself", () => {
    expect(parseMarkdown("смотри:\n```\nx\n```\nвот")).toEqual([
      text("смотри:"),
      { type: "codeBlock", lang: null, value: "x" },
      text("вот"),
    ]);
  });

  it("reads quotes, one line with > and the rest with >>>", () => {
    expect(parseMarkdown("> цитата\n> вторая\nответ")).toEqual([
      { type: "quote", children: [text("цитата\nвторая")] },
      text("ответ"),
    ]);
    expect(parseMarkdown("до\n>>> всё\nостальное")).toEqual([
      text("до"),
      { type: "quote", children: [text("всё\nостальное")] },
    ]);
  });

  it("links bare http(s) addresses, without the punctuation after them", () => {
    expect(parseMarkdown("см. https://example.com/a?b=1.")).toEqual([
      text("см. "),
      { type: "link", href: "https://example.com/a?b=1", children: [text("https://example.com/a?b=1")] },
      text("."),
    ]);
  });

  it("keeps balanced parentheses in a bare address", () => {
    const url = "https://en.wikipedia.org/wiki/Go_(язык)";
    expect(parseMarkdown(`(${url})`)).toEqual([
      text("("),
      { type: "link", href: new URL(url).href, children: [text(url)] },
      text(")"),
    ]);
  });

  it("reads a masked link with styled text", () => {
    expect(parseMarkdown("[**тут**](https://example.com)")).toEqual([
      { type: "link", href: "https://example.com/", children: [{ type: "bold", children: [text("тут")] }] },
    ]);
  });

  it.each([
    "[x](javascript:alert(1))",
    "[x](data:text/html,<script>alert(1)</script>)",
    "[x](vbscript:msgbox(1))",
    "[x](//evil.example)",
    "javascript:alert(1)",
  ])("never links %s", (src) => {
    expect(parseMarkdown(src)).toEqual([text(src)]);
  });

  it("keeps HTML as text", () => {
    expect(parseMarkdown("<script>alert(1)</script>")).toEqual([text("<script>alert(1)</script>")]);
  });

  it("reads escaped markdown characters literally", () => {
    expect(parseMarkdown("\\*не курсив\\*")).toEqual([text("*не курсив*")]);
  });

  it(`parses 9 levels of styles without failing and nests at most ${MAX_DEPTH}`, () => {
    const depthOf = (nodes: MdNode[]): number =>
      Math.max(0, ...nodes.map((n) => ("children" in n ? 1 + depthOf(n.children) : 0)));
    const open = ["||", "**", "__", "~~", "*", "||", "**", "__", "~~"];
    const src = `${open.join("")}x${[...open].reverse().join("")}`;
    expect(depthOf(parseMarkdown(src))).toBeLessThanOrEqual(MAX_DEPTH);
    const deep = `${"> ".repeat(3)}${"||**__~~*".repeat(20)}x`;
    expect(depthOf(parseMarkdown(deep))).toBeLessThanOrEqual(MAX_DEPTH);
  });

  it.each([
    ["4000 characters without spaces", "a".repeat(4000)],
    ["4000 emoji", "😀".repeat(4000)],
    ["4000 unmatched openers", "[*_~|`".repeat(667)],
  ])("parses %s in under 50 ms", (_, src) => {
    const start = performance.now();
    parseMarkdown(src);
    expect(performance.now() - start).toBeLessThan(50);
  });
});

describe("Markdown", () => {
  it("renders HTML as text and runs nothing", () => {
    renderUi(<Markdown source={"<script>window.__x = 1</script><b>не жирный</b>"} />);
    expect(screen.getByText("<script>window.__x = 1</script><b>не жирный</b>")).toBeTruthy();
    expect(document.querySelector("script, b")).toBeNull();
  });

  it("opens links in a new tab without passing the page on", () => {
    renderUi(<Markdown source="[сайт](https://example.com/путь)" />);
    const link = screen.getByRole("link", { name: "сайт" });
    expect(link.getAttribute("href")).toBe("https://example.com/%D0%BF%D1%83%D1%82%D1%8C");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer nofollow");
    // A masked link shows where it really goes.
    expect(link.getAttribute("title")).toBe("https://example.com/%D0%BF%D1%83%D1%82%D1%8C");
  });

  it("reveals a spoiler on click and with the keyboard", async () => {
    const { user } = renderUi(
      <>
        <Markdown source="||первый||" />
        <Markdown source="||второй||" />
      </>,
    );
    const [first, second] = screen.getAllByRole("button", { name: "Спойлер, нажмите, чтобы показать" });
    if (!first || !second) throw new Error("two spoilers expected");
    await user.click(first);
    second.focus();
    await user.keyboard("{Enter}");
    // Revealed, a spoiler is plain text again, read like the rest of the message.
    expect(screen.queryAllByRole("button")).toHaveLength(0);
    expect(screen.getByText("первый")).toBeTruthy();
    expect(screen.getByText("второй")).toBeTruthy();
  });

  it("keeps bidi control characters as text", () => {
    const rlo = cp(0x202e);
    renderUi(<Markdown source={`a${rlo}b`} />);
    expect(screen.getByText(`a${rlo}b`)).toBeTruthy();
  });
});
