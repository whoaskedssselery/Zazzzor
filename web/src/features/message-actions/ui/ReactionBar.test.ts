import { reactorsText } from "./ReactionBar";

describe("reactorsText", () => {
  it("names up to three and counts the rest", () => {
    expect(reactorsText(["Аня"], 1)).toBe("Аня");
    expect(reactorsText(["Аня", "Пётр"], 2)).toBe("Аня и Пётр");
    expect(reactorsText(["Аня", "Пётр", "Маша"], 3)).toBe("Аня, Пётр и Маша");
    expect(reactorsText(["Аня", "Пётр", "Маша"], 7)).toBe("Аня, Пётр, Маша и ещё 4");
  });
});
