import { formatMessageTime } from "./time";

// Local-time constructors, so the tests hold in any time zone.
const now = new Date(2026, 9, 23, 23, 30);

describe("formatMessageTime", () => {
  it("says «Сегодня» for the same calendar day", () => {
    expect(formatMessageTime(new Date(2026, 9, 23, 21, 4), now)).toBe("Сегодня в 21:04");
    expect(formatMessageTime(new Date(2026, 9, 23, 0, 0), now)).toBe("Сегодня в 00:00");
  });

  it("says «Вчера» for the previous calendar day", () => {
    expect(formatMessageTime(new Date(2026, 9, 22, 21, 4), now)).toBe("Вчера в 21:04");
    expect(formatMessageTime(new Date(2026, 9, 31, 23, 59), new Date(2026, 10, 1, 0, 1))).toBe("Вчера в 23:59");
  });

  it("says «Вчера» across a year boundary", () => {
    expect(formatMessageTime(new Date(2026, 11, 31, 23, 59), new Date(2027, 0, 1, 0, 1))).toBe("Вчера в 23:59");
  });

  it("gives the full date for anything older", () => {
    expect(formatMessageTime(new Date(2026, 9, 21, 21, 4), now)).toBe("21.10.2026 21:04");
    expect(formatMessageTime(new Date(2025, 0, 2, 9, 5), now)).toBe("02.01.2025 09:05");
  });

  it("gives the full date for a time ahead of the local clock", () => {
    expect(formatMessageTime(new Date(2026, 9, 24, 21, 4), now)).toBe("24.10.2026 21:04");
  });

  it("matches the Discord examples", () => {
    const evening = new Date(2026, 9, 23, 21, 4);
    expect(formatMessageTime(evening, new Date(2026, 9, 23, 22, 0))).toBe("Сегодня в 21:04");
    expect(formatMessageTime(evening, new Date(2026, 9, 24, 10, 0))).toBe("Вчера в 21:04");
    expect(formatMessageTime(evening, new Date(2026, 9, 26, 10, 0))).toBe("23.10.2026 21:04");
  });
});
