import { makeMessage, makeUser } from "@/shared/test";
import { emojiKey, emojiOfKey, sameEmoji, withReaction } from "./reactions";

const m = makeMessage("1", "101", makeUser("2"));
const LIKE = { id: null, name: "\u{1F44D}\u{FE0F}" };
const FIRE = { id: null, name: "\u{1F525}" };
const VASYA = { id: "77", name: "vasya" };

describe("withReaction", () => {
  it("adds a new emoji last and counts others on an existing one", () => {
    let x = withReaction(m, LIKE, false, true);
    x = withReaction(x, FIRE, false, true);
    x = withReaction(x, LIKE, false, true);
    expect(x.reactions).toEqual([
      { emoji: LIKE, count: 2, me: false },
      { emoji: FIRE, count: 1, me: false },
    ]);
  });

  it("takes the user's own change as a state: their echoed event changes nothing", () => {
    const mine = withReaction(withReaction(m, LIKE, false, true), LIKE, true, true);
    expect(withReaction(mine, LIKE, true, true)).toBe(mine);
    const gone = withReaction(mine, LIKE, true, false);
    expect(gone.reactions).toEqual([{ emoji: LIKE, count: 1, me: false }]);
    expect(withReaction(gone, LIKE, true, false)).toBe(gone);
  });

  it("drops an emoji whose last reaction goes, and ignores the removal of one not there", () => {
    const one = withReaction(m, FIRE, false, true);
    expect(withReaction(one, FIRE, false, false).reactions).toEqual([]);
    expect(withReaction(m, FIRE, false, false)).toBe(m);
  });

  it("counts a renamed custom emoji with the reactions it had", () => {
    const x = withReaction(withReaction(m, VASYA, false, true), { id: "77", name: "vasily" }, false, true);
    expect(x.reactions).toEqual([{ emoji: VASYA, count: 2, me: false }]);
  });
});

describe("emoji keys", () => {
  it("names Unicode emoji as themselves and custom ones as name:id, and reads them back", () => {
    expect(emojiKey(LIKE)).toBe(LIKE.name);
    expect(emojiKey(VASYA)).toBe("vasya:77");
    expect(emojiOfKey("vasya:77")).toEqual(VASYA);
    expect(emojiOfKey(LIKE.name)).toEqual(LIKE);
  });

  it("tells custom emoji by ID and Unicode ones by their string", () => {
    expect(sameEmoji(VASYA, { id: "77", name: "other" })).toBe(true);
    expect(sameEmoji(VASYA, { id: null, name: "vasya" })).toBe(false);
    expect(sameEmoji(LIKE, FIRE)).toBe(false);
  });
});
