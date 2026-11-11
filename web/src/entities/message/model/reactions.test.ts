import { makeMessage, makeUser } from "@/shared/test";
import { withReaction } from "./reactions";

const m = makeMessage("1", "101", makeUser("2"));
const LIKE = "\u{1F44D}\u{FE0F}";
const FIRE = "\u{1F525}";

describe("withReaction", () => {
  it("adds a new emoji last and counts others on an existing one", () => {
    let x = withReaction(m, LIKE, false, true);
    x = withReaction(x, FIRE, false, true);
    x = withReaction(x, LIKE, false, true);
    expect(x.reactions).toEqual([
      { emoji: { id: null, name: LIKE }, count: 2, me: false },
      { emoji: { id: null, name: FIRE }, count: 1, me: false },
    ]);
  });

  it("takes the user's own change as a state: their echoed event changes nothing", () => {
    const mine = withReaction(withReaction(m, LIKE, false, true), LIKE, true, true);
    expect(withReaction(mine, LIKE, true, true)).toBe(mine);
    const gone = withReaction(mine, LIKE, true, false);
    expect(gone.reactions).toEqual([{ emoji: { id: null, name: LIKE }, count: 1, me: false }]);
    expect(withReaction(gone, LIKE, true, false)).toBe(gone);
  });

  it("drops an emoji whose last reaction goes, and ignores the removal of one not there", () => {
    const one = withReaction(m, FIRE, false, true);
    expect(withReaction(one, FIRE, false, false).reactions).toEqual([]);
    expect(withReaction(m, FIRE, false, false)).toBe(m);
  });
});
