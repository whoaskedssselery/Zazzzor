import { runeLength, trimContent } from "./text";

const cp = (...codes: number[]) => String.fromCodePoint(...codes);

describe("runeLength", () => {
  it("counts code points, not UTF-16 units", () => {
    expect(runeLength("👍".repeat(4000))).toBe(4000);
    expect(runeLength("привет")).toBe(6);
    expect(runeLength("")).toBe(0);
  });

  it("counts a combining mark as its own rune, like the server", () => {
    expect(runeLength(`e${cp(0x301)}`)).toBe(2);
  });
});

describe("trimContent", () => {
  it("trims ASCII and ideographic spaces", () => {
    expect(trimContent(`  a ${cp(0x3000)}`)).toBe("a");
  });

  it("trims every character Go's unicode.IsSpace accepts", () => {
    // biome-ignore format: one row per Unicode block, the same rows as GO_SPACES in text.ts.
    const spaces = cp(
      0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x20, 0x85, 0xa0, 0x1680,
      0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a,
      0x2028, 0x2029, 0x202f, 0x205f, 0x3000,
    );
    expect(trimContent(`${spaces}текст${spaces}`)).toBe("текст");
    expect(trimContent(spaces)).toBe("");
  });

  it("keeps inner whitespace", () => {
    expect(trimContent(" a  b\nc ")).toBe("a  b\nc");
  });

  it("keeps characters unicode.IsSpace rejects (zero-width space, BOM)", () => {
    const zwsp = cp(0x200b);
    const bom = cp(0xfeff);
    expect(trimContent(`${zwsp}a${bom}`)).toBe(`${zwsp}a${bom}`);
  });
});
