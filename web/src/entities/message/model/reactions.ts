import type { components } from "@/shared/api";
import type { Message } from "./cache";

export type Reaction = components["schemas"]["Reaction"];
export type ReactionEmoji = components["schemas"]["Emoji"];

/** A message's reactions; an event's copy of a message may carry none. */
export const reactionsOf = (m: Pick<Message, "reactions">): Reaction[] => m.reactions ?? [];

/**
 * An emoji as the reactions API names it: a Unicode emoji as itself, a
 * custom one as `name:id` (C11). The client passes reactions around in
 * this form.
 */
export const emojiKey = (e: ReactionEmoji): string => (e.id === null ? e.name : `${e.name}:${e.id}`);

const CUSTOM_KEY = /^([A-Za-z0-9_]{2,32}):([0-9]{1,20})$/;

/** The emoji of a key: `name:id` is a custom one, anything else Unicode. */
export const emojiOfKey = (key: string): ReactionEmoji => {
  const m = CUSTOM_KEY.exec(key);
  return m ? { id: m[2] ?? "", name: m[1] ?? "" } : { id: null, name: key };
};

/** Whether `a` and `b` are one emoji: a custom one by its ID, whatever its name now; Unicode by its string. */
export const sameEmoji = (a: ReactionEmoji, b: ReactionEmoji): boolean =>
  a.id !== null || b.id !== null ? a.id === b.id : a.name === b.name;

/**
 * `m` after someone added (`add`) or took back a reaction `emoji`; `mine`
 * when that someone is the user. The user's own change is a state, not a
 * step: the chat shows it at once and the server's event of it then
 * changes nothing. A new emoji goes last, as the server orders them.
 *
 * With `count` (the server's event says how many reacted after the
 * change) the count is set, not stepped: an event seen twice, as a
 * reconnect's catch-up replays them, changes nothing the second time.
 */
export function withReaction(m: Message, emoji: ReactionEmoji, mine: boolean, add: boolean, count?: number): Message {
  const reactions = reactionsOf(m);
  const at = reactions.findIndex((r) => sameEmoji(r.emoji, emoji));
  const held = reactions[at];
  if (count !== undefined) {
    const me = mine ? add : (held?.me ?? false);
    if (count <= 0) return held === undefined ? m : { ...m, reactions: reactions.filter((_, i) => i !== at) };
    if (held === undefined) return { ...m, reactions: [...reactions, { emoji, count, me }] };
    if (held.count === count && held.me === me) return m;
    return { ...m, reactions: reactions.map((r, i) => (i === at ? { ...held, count, me } : r)) };
  }
  if (add) {
    if (held === undefined) return { ...m, reactions: [...reactions, { emoji, count: 1, me: mine }] };
    if (mine && held.me) return m;
    const next = { ...held, count: held.count + 1, me: held.me || mine };
    return { ...m, reactions: reactions.map((r, i) => (i === at ? next : r)) };
  }
  if (held === undefined || (mine && !held.me)) return m;
  if (held.count <= 1) return { ...m, reactions: reactions.filter((_, i) => i !== at) };
  const next = { ...held, count: held.count - 1, me: mine ? false : held.me };
  return { ...m, reactions: reactions.map((r, i) => (i === at ? next : r)) };
}
