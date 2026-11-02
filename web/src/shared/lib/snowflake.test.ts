import { compareIds, snowflakeTime } from "./snowflake";

const EPOCH_MS = Date.UTC(2026, 0, 1);

describe("snowflakeTime", () => {
  it("decodes the example ID of the contract", () => {
    // api/openapi.yaml, components.schemas.Snowflake.examples
    expect(snowflakeTime("1234567890123456789").toISOString()).toBe("2035-04-30T18:12:02.167Z");
  });

  it("returns the epoch for an ID made in its first millisecond", () => {
    expect(snowflakeTime("4095").getTime()).toBe(EPOCH_MS);
    expect(snowflakeTime(String(1n << 22n)).getTime()).toBe(EPOCH_MS + 1);
  });

  it("keeps full precision for IDs above 2^53", () => {
    // Number("9223372036854775807") rounds up to 2^63, one millisecond too late.
    const max = (1n << 63n) - 1n;
    expect(max > BigInt(Number.MAX_SAFE_INTEGER)).toBe(true);
    expect(snowflakeTime(max.toString()).getTime()).toBe(EPOCH_MS + 2 ** 41 - 1);
  });
});

describe("compareIds", () => {
  it("orders snowflakes as numbers, also past 2^53 and across lengths", () => {
    expect(compareIds("9", "10")).toBeLessThan(0);
    expect(compareIds("9007199254740993", "9007199254740992")).toBeGreaterThan(0);
    expect(compareIds("123", "123")).toBe(0);
  });
});
