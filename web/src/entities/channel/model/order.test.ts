import { makeChannel } from "@/shared/test";
import { channelMove, stepMove } from "./order";

const G = "10";
const a = makeChannel("101", G, { parent_id: "30", position: 0 });
const b = makeChannel("102", G, { parent_id: "30", position: 1 });
const c = makeChannel("103", G, { parent_id: "30", position: 2 });
const x = makeChannel("201", G, { parent_id: "31", position: 0 });

describe("channelMove", () => {
  it("moves up within a parent to the position of the channel it lands before", () => {
    expect(channelMove(c, "30", a)).toEqual({ id: c.id, position: 0 });
  });

  it("moves down within a parent one position earlier, as the server splices", () => {
    expect(channelMove(a, "30", c)).toEqual({ id: a.id, position: 1 });
  });

  it("moves to the end, and into another parent with its ID", () => {
    expect(channelMove(a, "30", null)).toEqual({ id: a.id, position: 9999 });
    expect(channelMove(a, "31", x)).toEqual({ id: a.id, position: 0, parent_id: "31" });
    expect(channelMove(a, null, null)).toEqual({ id: a.id, position: 9999, parent_id: null });
  });

  it("asks for nothing when the channel stays where it is", () => {
    expect(channelMove(a, "30", b)).toBeNull();
    expect(channelMove(a, "30", a)).toBeNull();
  });
});

describe("stepMove", () => {
  it("steps one place up or down within the group, and not past its edges", () => {
    const group = [a, b, c];
    expect(stepMove(b, group, true)).toEqual({ id: b.id, position: 0 });
    expect(stepMove(b, group, false)).toEqual({ id: b.id, position: 9999 });
    expect(stepMove(a, group, false)).toEqual({ id: a.id, position: 1 });
    expect(stepMove(a, group, true)).toBeNull();
    expect(stepMove(c, group, false)).toBeNull();
  });
});
