import { DEFAULT_RECENT, groupEmoji, recentEmoji, rememberEmoji, searchEmoji } from "./emoji";

const raw = [
  { emoji: "😂", label: "смех до слёз", tags: ["радость", "смех"], group: 0, order: 2, version: 0.6 },
  { emoji: "😀", label: "широкая улыбка", tags: ["лицо", "улыбка"], group: 0, order: 1, version: 1 },
  { emoji: "👍️", label: "большой палец вверх", tags: ["лайк", "нравится"], group: 1, order: 3, version: 0.6 },
  { emoji: "🫨", label: "трясущееся лицо", tags: ["шок"], group: 0, order: 4, version: 15 },
  { emoji: "🙂‍↔️", label: "мотает головой", tags: ["нет"], group: 0, order: 5, version: 15.1 },
  { emoji: "🏻", label: "светлый тон кожи", group: 2, order: 6, version: 1 },
  { emoji: "🇦", label: "буква A", order: 7, version: 0 },
];

describe("emoji", () => {
  it("groups by emojibase's order, without skin tone parts, loose letters or emoji newer than Unicode 15", () => {
    const groups = groupEmoji(raw);
    expect(groups.map((g) => [g.name, g.emoji.map((e) => e.emoji)])).toEqual([
      ["Смайлики и эмоции", ["😀", "😂", "🫨"]],
      ["Люди и жесты", ["👍️"]],
    ]);
  });

  it("finds by any word of the name or the keywords, in Russian", () => {
    const groups = groupEmoji(raw);
    expect(searchEmoji(groups, "лайк").map((e) => e.emoji)).toEqual(["👍️"]);
    expect(searchEmoji(groups, "Смех радость").map((e) => e.emoji)).toEqual(["😂"]);
    expect(searchEmoji(groups, "  ")).toEqual([]);
  });

  it("remembers picked emoji newest first, starting from the defaults", () => {
    localStorage.clear();
    expect(recentEmoji()).toEqual(DEFAULT_RECENT);
    rememberEmoji("😂");
    rememberEmoji("🔥");
    rememberEmoji("😂");
    expect(recentEmoji().slice(0, 3)).toEqual(["😂", "🔥", DEFAULT_RECENT[0]]);
  });
});
