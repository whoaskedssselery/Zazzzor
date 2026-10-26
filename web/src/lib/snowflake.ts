/** Zero point of the snowflake timestamp bits (server/internal/platform/snowflake). */
const EPOCH_MS = Date.UTC(2026, 0, 1);
const TIME_SHIFT = 22n;

/**
 * Creation time encoded in a snowflake ID. IDs stay strings everywhere and are
 * only parsed as BigInt here: a Number loses precision above 2^53.
 */
export function snowflakeTime(id: string): Date {
  return new Date(EPOCH_MS + Number(BigInt(id) >> TIME_SHIFT));
}
