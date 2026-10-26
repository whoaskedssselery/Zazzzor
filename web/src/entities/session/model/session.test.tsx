import { safeNext } from "./session";

describe("safeNext", () => {
  it.each([
    ["/invite/abc", "/invite/abc"],
    ["/channels/1/2?x=1#m", "/channels/1/2?x=1#m"],
    ["/settings/profile", "/settings/profile"],
  ])("keeps the same-origin path %s", (next, expected) => {
    expect(safeNext(next)).toBe(expected);
  });

  it.each([
    ["//evil.com"],
    ["//evil.com/channels/@me"],
    ["/.//evil.com"],
    ["/%2e//evil.com"],
    ["/%2e%2e//evil.com"],
    ["/a/..//evil.com"],
    ["/./\\evil.com"],
    [`/.${String.fromCodePoint(9)}//evil.com`],
    [`/a/..${String.fromCodePoint(10)}//evil.com`],
    ["/\\evil.com"],
    ["\\\\evil.com"],
    ["https://evil.com"],
    ["javascript:alert(1)"],
    ["evil.com"],
    ["channels/@me"],
    [""],
    [`/${String.fromCodePoint(9)}/evil.com`],
    [`/${String.fromCodePoint(10)}/evil.com`],
    ["/login"],
    ["/login?next=/invite/abc"],
    [undefined],
    [42],
  ])("falls back to /channels/@me for %j", (next) => {
    expect(safeNext(next)).toBe("/channels/@me");
  });
});
