import { shareReducer } from "./share-state";

test("happy path", () => {
  let s = shareReducer({ status: "idle" }, { type: "start" });
  expect(s).toEqual({ status: "starting" });
  s = shareReducer(s, { type: "started" });
  expect(s).toEqual({ status: "sharing" });
  expect(shareReducer(s, { type: "stopped" })).toEqual({ status: "idle" });
});
test("cancelling the picker returns to idle without error", () => {
  expect(shareReducer({ status: "starting" }, { type: "cancelled" })).toEqual({ status: "idle" });
});
test("failure keeps message and allows retry", () => {
  const s = shareReducer({ status: "starting" }, { type: "failed", message: "x" });
  expect(s).toEqual({ status: "error", message: "x" });
  expect(shareReducer(s, { type: "start" })).toEqual({ status: "starting" });
});
test("start while sharing is ignored", () => {
  expect(shareReducer({ status: "sharing" }, { type: "start" })).toEqual({ status: "sharing" });
});
