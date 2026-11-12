import { type ComposerEmoji, emojiQuery, encodeEmoji, rankEmoji } from "./emoji";

const e = (id: string, name: string, animated = false): ComposerEmoji => ({ id, name, animated, url: `/e/${id}` });
const VASYA = e("77", "vasya");
const DANCE = e("78", "dance", true);
const ALL = [VASYA, DANCE, e("79", "old_vasya")];

describe("emojiQuery", () => {
  it("finds :query at a word start, two characters on", () => {
    expect(emojiQuery("привет :va", 10)).toEqual({ start: 7, query: "va" });
    expect(emojiQuery(":va", 3)).toEqual({ start: 0, query: "va" });
  });

  it("is null for one character, a colon inside a word or a time, and after a space", () => {
    expect(emojiQuery(":v", 2)).toBeNull();
    expect(emojiQuery("в 10:30", 7)).toBeNull();
    expect(emojiQuery("a:vasya", 7)).toBeNull();
    expect(emojiQuery(":va sya", 7)).toBeNull();
  });
});

describe("rankEmoji", () => {
  it("puts names that start with the query before those that contain it, case aside", () => {
    expect(rankEmoji(ALL, "VAS").map((x) => x.name)).toEqual(["vasya", "old_vasya"]);
  });
});

describe("encodeEmoji", () => {
  it("turns :name: of the server's emoji into their syntax, animated ones with a", () => {
    expect(encodeEmoji("ну :vasya: и :Dance:", ALL)).toBe("ну <:vasya:77> и <a:dance:78>");
  });

  it("leaves unknown names, code and the server's syntax as they are", () => {
    const text = "`:vasya:` :nope: <:vasya:77>\n```\n:vasya:\n```";
    expect(encodeEmoji(text, ALL)).toBe(text);
  });
});
