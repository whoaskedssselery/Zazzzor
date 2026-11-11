import {
  encodeMentions,
  MAX_SUGGESTIONS,
  type MentionSuggestion,
  mentionQuery,
  rankSuggestions,
  rememberMention,
  resetMentions,
} from "./mentions";

const user = (id: string, name: string, detail = name.toLowerCase()): MentionSuggestion => ({
  kind: "user",
  id,
  name,
  detail,
});

afterEach(() => {
  resetMentions();
});

describe("mentionQuery", () => {
  it("finds the @query the caret is in, at a word start", () => {
    expect(mentionQuery("привет @ан", 10)).toEqual({ start: 7, query: "ан" });
    expect(mentionQuery("@", 1)).toEqual({ start: 0, query: "" });
  });

  it("is none inside a word, after a space, or when the caret is elsewhere", () => {
    expect(mentionQuery("mail@an", 7)).toBeNull();
    expect(mentionQuery("@аня петрова", 12)).toBeNull();
    expect(mentionQuery("@ан и ещё", 9)).toBeNull();
  });
});

describe("rankSuggestions", () => {
  const all = [user("1", "Аня", "anya"), user("2", "Маня", "manya"), user("3", "Пётр", "anya_fan")];

  it("puts names that start with the query first, then those that contain it, matching usernames too", () => {
    expect(rankSuggestions(all, "ан").map((s) => s.name)).toEqual(["Аня", "Маня"]);
    expect(rankSuggestions(all, "ма").map((s) => s.name)).toEqual(["Маня"]);
    expect(rankSuggestions(all, "anya").map((s) => s.name)).toEqual(["Аня", "Пётр", "Маня"]);
  });

  it("shows at most eight", () => {
    const many = Array.from({ length: 20 }, (_, i) => user(String(i), `Друг ${String(i)}`));
    expect(rankSuggestions(many, "")).toHaveLength(MAX_SUGGESTIONS);
  });
});

describe("encodeMentions", () => {
  it("turns picked names into the server's tokens, whole words only, longest first", () => {
    rememberMention("101", user("1", "Аня"));
    rememberMention("101", user("2", "Аня Петрова"));
    rememberMention("101", { kind: "role", id: "7", name: "Модер" });
    expect(encodeMentions("101", "@Аня Петрова и @Аня, @Модер @Анька")).toBe("<@2> и <@1>, <@&7> @Анька");
  });

  it("leaves names typed by hand and other channels alone, and forgets after use", () => {
    rememberMention("101", user("1", "Аня"));
    expect(encodeMentions("102", "@Аня")).toBe("@Аня");
    expect(encodeMentions("101", "@Аня")).toBe("<@1>");
    expect(encodeMentions("101", "@Аня")).toBe("@Аня");
  });
});
