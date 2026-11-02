import type { Message, PendingMessage } from "@/entities/message";

/** A message continues the group above it when it follows the same author within this long. */
export const GROUP_MS = 7 * 60_000;

/** One row of the chat list. Keys stay with their content, so measured heights survive inserts above. */
export type ChatRow =
  | { kind: "start"; key: "start" }
  | { kind: "older"; key: "older" }
  | { kind: "day"; key: string; label: string }
  | { kind: "message"; key: string; message: Message; grouped: boolean }
  | { kind: "pending"; key: string; pending: PendingMessage; grouped: boolean };

const dayFormat = new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "long", year: "numeric" });

const dayKey = (d: Date) => `${String(d.getFullYear())}-${String(d.getMonth())}-${String(d.getDate())}`;

/** Whether `m` continues the group of `prev`: same author, no reply, same day, within GROUP_MS. */
export const continues = (prev: Message | undefined, m: Message): boolean => {
  if (prev === undefined || m.author === null || prev.author?.id !== m.author.id || m.reply_to_id !== null) {
    return false;
  }
  const a = new Date(prev.created_at);
  const b = new Date(m.created_at);
  const gap = b.getTime() - a.getTime();
  return gap >= 0 && gap < GROUP_MS && dayKey(a) === dayKey(b);
};

/**
 * The rows of the chat, top to bottom: the start of the channel once no
 * older history is left (else a row that loads it), each day's separator,
 * the messages grouped as Discord groups them, then the user's messages
 * still being sent (`meId` is the user), which join the last group when it
 * is theirs and recent.
 */
export function buildRows(
  messages: Message[],
  pending: PendingMessage[],
  { hasOlder, meId, now }: { hasOlder: boolean; meId: string | null; now: Date },
): ChatRow[] {
  const rows: ChatRow[] = [hasOlder ? { kind: "older", key: "older" } : { kind: "start", key: "start" }];
  let prev: Message | undefined;
  for (const m of messages) {
    const at = new Date(m.created_at);
    if (prev === undefined || dayKey(new Date(prev.created_at)) !== dayKey(at)) {
      rows.push({ kind: "day", key: `day-${dayKey(at)}`, label: dayFormat.format(at) });
    }
    rows.push({ kind: "message", key: m.id, message: m, grouped: continues(prev, m) });
    prev = m;
  }
  let mine =
    prev !== undefined &&
    prev.author?.id === meId &&
    now.getTime() - new Date(prev.created_at).getTime() < GROUP_MS &&
    dayKey(new Date(prev.created_at)) === dayKey(now);
  for (const p of pending) {
    const grouped = mine && p.replyTo === null;
    rows.push({ kind: "pending", key: `pending-${p.nonce}`, pending: p, grouped });
    mine = true;
  }
  return rows;
}
