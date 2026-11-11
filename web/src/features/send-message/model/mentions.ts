/**
 * Mentions in the composer. The field shows `@Name`, as Discord's does; the
 * message carries the server's syntax (`<@id>`, `<@&id>`), so picked names
 * are remembered per channel and encoded when the message is sent. A name
 * typed by hand, never picked, stays plain text.
 */

/** Something the user may mention, as the suggestion list shows it. */
export interface MentionSuggestion {
  kind: "user" | "role" | "everyone" | "here";
  /** User or role ID; null for @everyone and @here. */
  id: string | null;
  /** The name inserted after `@`, and shown first. */
  name: string;
  /** A second line: the username, or what @everyone and @here reach. */
  detail?: string;
  avatarUrl?: string | null;
  /** A role's colour (0: none). */
  color?: number;
}

/** Suggestions shown at most. */
export const MAX_SUGGESTIONS = 8;
/** The query after `@` is this long at most; longer, it is ordinary text. */
const MAX_QUERY = 32;

/**
 * The `@query` the caret is in: the `@` at a word start and what follows it
 * up to the caret, with no space in between. Null when the caret is not in one.
 */
export const mentionQuery = (text: string, caret: number): { start: number; query: string } | null => {
  const at = text.lastIndexOf("@", caret - 1);
  if (at === -1) return null;
  const query = text.slice(at + 1, caret);
  if (query.length > MAX_QUERY || /\s/.test(query)) return null;
  const before = text[at - 1];
  if (before !== undefined && !/\s/.test(before)) return null;
  return { start: at, query };
};

const fold = (s: string) => s.toLocaleLowerCase("ru-RU");

/** Suggestions whose name or detail starts with the query come first, then those that contain it. */
export const rankSuggestions = (all: readonly MentionSuggestion[], query: string): MentionSuggestion[] => {
  const q = fold(query);
  const starts: MentionSuggestion[] = [];
  const contains: MentionSuggestion[] = [];
  for (const s of all) {
    const name = fold(s.name);
    const detail = fold(s.detail ?? "");
    if (name.startsWith(q) || detail.startsWith(q)) starts.push(s);
    else if (q !== "" && (name.includes(q) || detail.includes(q))) contains.push(s);
  }
  return [...starts, ...contains].slice(0, MAX_SUGGESTIONS);
};

/** The server's token for a suggestion. */
const token = (s: MentionSuggestion): string => {
  switch (s.kind) {
    case "user":
      return `<@${s.id ?? ""}>`;
    case "role":
      return `<@&${s.id ?? ""}>`;
    default:
      return `@${s.kind}`;
  }
};

/** Picked names by channel: `@Name` → token. */
const picked = new Map<string, Map<string, string>>();

/** Remembers that `@name` in the composer of `channelId` means `s`. */
export const rememberMention = (channelId: string, s: MentionSuggestion): void => {
  if (s.kind === "everyone" || s.kind === "here") return;
  const map = picked.get(channelId) ?? new Map<string, string>();
  map.set(`@${s.name}`, token(s));
  picked.set(channelId, map);
};

const WORD = /[\p{L}\p{N}_]/u;

/**
 * The text of the composer of `channelId` as the server reads it: every
 * picked `@Name` still standing as a whole word becomes its token, longer
 * names first so «@Аня Петрова» is not taken for «@Аня». Then forgets them.
 */
export const encodeMentions = (channelId: string, text: string): string => {
  const map = picked.get(channelId);
  picked.delete(channelId);
  if (!map || map.size === 0) return text;
  let out = text;
  for (const [shown, tok] of [...map].sort((a, b) => b[0].length - a[0].length)) {
    let from = 0;
    for (;;) {
      const at = out.indexOf(shown, from);
      if (at === -1) break;
      const end = at + shown.length;
      const whole = !WORD.test(out[end] ?? "") && (at === 0 || !WORD.test(out[at - 1] ?? ""));
      if (whole) {
        out = out.slice(0, at) + tok + out.slice(end);
        from = at + tok.length;
      } else from = end;
    }
  }
  return out;
};

/** Forgets every picked mention (the session ended). */
export const resetMentions = (): void => {
  picked.clear();
};
