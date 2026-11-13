/**
 * What just arrived over the gateway: a message from someone else, a
 * reaction that started a new chip. Their rows animate in; everything that
 * was already there (the history on open, a row mounted again while
 * scrolling) does not, since it is no longer "just".
 */

/** How long after arriving something still counts as new, in ms. */
const FRESH_MS = 700;

const arrived = new Map<string, number>();

const prune = (now: number): void => {
  for (const [key, at] of arrived) {
    if (now - at > FRESH_MS) arrived.delete(key);
  }
};

/** Notes that `key` arrived now. */
export const markArrived = (key: string): void => {
  const now = performance.now();
  prune(now);
  arrived.set(key, now);
};

/** Whether `key` arrived within the last moment. */
export const justArrived = (key: string): boolean => {
  const at = arrived.get(key);
  return at !== undefined && performance.now() - at < FRESH_MS;
};

/** The key of a reaction chip: its message and emoji. */
export const reactionKey = (messageId: string, emoji: string): string => `${messageId}:${emoji}`;
