import type { components } from "@/shared/api";
import type { Message } from "./cache";

export type Reaction = components["schemas"]["Reaction"];

/** A message's reactions; an event's copy of a message may carry none. */
export const reactionsOf = (m: Pick<Message, "reactions">): Reaction[] => m.reactions ?? [];

/**
 * `m` after someone added (`add`) or took back a reaction `emoji`; `mine`
 * when that someone is the user. The user's own change is a state, not a
 * step: the chat shows it at once and the server's event of it then
 * changes nothing. A new emoji goes last, as the server orders them.
 */
export function withReaction(m: Message, emoji: string, mine: boolean, add: boolean): Message {
  const reactions = reactionsOf(m);
  const at = reactions.findIndex((r) => r.emoji.name === emoji);
  const held = reactions[at];
  if (add) {
    if (held === undefined)
      return { ...m, reactions: [...reactions, { emoji: { id: null, name: emoji }, count: 1, me: mine }] };
    if (mine && held.me) return m;
    const next = { ...held, count: held.count + 1, me: held.me || mine };
    return { ...m, reactions: reactions.map((r, i) => (i === at ? next : r)) };
  }
  if (held === undefined || (mine && !held.me)) return m;
  if (held.count <= 1) return { ...m, reactions: reactions.filter((_, i) => i !== at) };
  const next = { ...held, count: held.count - 1, me: mine ? false : held.me };
  return { ...m, reactions: reactions.map((r, i) => (i === at ? next : r)) };
}
