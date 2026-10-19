import { readTokenFromHash, tokenExpiresAt, isTokenExpired } from "./token";

const jwt = (payload: object) =>
  `h.${Buffer.from(JSON.stringify(payload)).toString("base64url")}.s`;

test("reads token from #token=", () => {
  expect(readTokenFromHash("#token=abc.def.ghi")).toBe("abc.def.ghi");
});
test("returns null for empty or foreign hash", () => {
  expect(readTokenFromHash("")).toBeNull();
  expect(readTokenFromHash("#foo=1")).toBeNull();
  expect(readTokenFromHash("#token=")).toBeNull();
});
test("reads exp as Date", () => {
  expect(tokenExpiresAt(jwt({ exp: 1_800_000_000 }))?.getTime()).toBe(1_800_000_000_000);
});
test("unreadable token has no expiry and counts as expired", () => {
  expect(tokenExpiresAt("garbage")).toBeNull();
  expect(isTokenExpired("garbage", new Date())).toBe(true);
});
test("expired vs valid token", () => {
  const now = new Date(1_800_000_000_000);
  expect(isTokenExpired(jwt({ exp: 1_799_999_999 }), now)).toBe(true);
  expect(isTokenExpired(jwt({ exp: 1_800_000_100 }), now)).toBe(false);
});
