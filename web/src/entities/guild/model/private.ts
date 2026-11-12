import { type DMChannel, type GuildsState, newer, PRIVATE_TYPE, type PrivateEntry } from "./reducer";

/**
 * What a private channel is called: a group by its name, or else by its
 * members (as Discord does: «Аня, Пётр, Маша»); a 1:1 by the other person.
 * A group the user is left alone in is «Только вы».
 */
export const privateName = (c: Pick<DMChannel, "name" | "recipients">): string => {
  if (c.name !== null) return c.name;
  if (c.recipients.length === 0) return "Только вы";
  return c.recipients.map((r) => r.display_name).join(", ");
};

/** The other person of a 1:1 DM; null for a group. */
export const dmPeer = (c: DMChannel) => (c.type === PRIVATE_TYPE.DM ? (c.recipients[0] ?? null) : null);

/** The newest activity of a private channel: its newest message, else when it was made (the ID). */
const activity = (p: PrivateEntry): string => p.channel.last_message_id ?? p.channel.id;

/** The user's open DMs and groups, the most recently active first, as Discord lists them. */
export const privateList = (s: Pick<GuildsState, "privates">): PrivateEntry[] =>
  Object.values(s.privates).sort((a, b) => {
    const x = activity(a);
    const y = activity(b);
    if (x === y) return 0;
    return newer(x, y) ? -1 : 1;
  });

/** Whether the private channel has messages the user has not read. */
export const isPrivateUnread = (p: PrivateEntry): boolean => {
  const last = p.channel.last_message_id;
  if (last === null) return false;
  return p.read === null || newer(last, p.read.lastReadId);
};

/** Unread messages from others in the private channel (each one counts, as Discord's DM badges). */
export const privateUnreadCount = (p: PrivateEntry): number => (isPrivateUnread(p) ? (p.read?.mentions ?? 0) : 0);

/** Unread messages across all of the user's DMs and groups: the badge on «Главная». */
export const homeUnreadCount = (s: Pick<GuildsState, "privates">): number =>
  Object.values(s.privates).reduce((sum, p) => sum + privateUnreadCount(p), 0);
