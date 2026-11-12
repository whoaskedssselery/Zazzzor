/**
 * Custom emoji in the composer, as in Discord: `:na` suggests the server's
 * emoji, a pick puts `:name:` in the field, and any `:name:` of this
 * server's emoji becomes `<:name:id>` (`<a:name:id>` when animated) when
 * the message is sent. Inside code it stays text.
 */

/** A custom emoji the composer can offer. */
export interface ComposerEmoji {
  id: string;
  name: string;
  animated: boolean;
  url: string;
}

/** Suggestions shown at most. */
export const MAX_EMOJI_SUGGESTIONS = 8;

const QUERY = /^[A-Za-z0-9_]{2,32}$/;

/**
 * The `:query` the caret is in: a `:` at a word start and at least two name
 * characters up to the caret. Null when the caret is not in one (`10:30`
 * is not: its colon follows a digit).
 */
export const emojiQuery = (text: string, caret: number): { start: number; query: string } | null => {
  const at = text.lastIndexOf(":", caret - 1);
  if (at === -1) return null;
  const query = text.slice(at + 1, caret);
  if (!QUERY.test(query)) return null;
  const before = text[at - 1];
  if (before !== undefined && !/\s/.test(before)) return null;
  return { start: at, query };
};

/** Emoji whose name starts with the query first, then those that contain it, case aside. */
export const rankEmoji = (all: readonly ComposerEmoji[], query: string): ComposerEmoji[] => {
  const q = query.toLowerCase();
  const starts = all.filter((e) => e.name.toLowerCase().startsWith(q));
  const contains = all.filter((e) => !e.name.toLowerCase().startsWith(q) && e.name.toLowerCase().includes(q));
  return [...starts, ...contains].slice(0, MAX_EMOJI_SUGGESTIONS);
};

/** Code spans and blocks, kept as they are, and the emoji forms of the text between them. */
const SEGMENTS = /(```[\s\S]*?```|`[^`\n]*`)/;
const FORMS = /<a?:[A-Za-z0-9_]{2,32}:[0-9]+>|:([A-Za-z0-9_]{2,32}):/g;

/** The message text with every `:name:` of `emojis` (names match case aside) in the server's syntax. */
export const encodeEmoji = (text: string, emojis: readonly ComposerEmoji[]): string => {
  if (emojis.length === 0 || !text.includes(":")) return text;
  const byName = new Map(emojis.map((e) => [e.name.toLowerCase(), e]));
  return text
    .split(SEGMENTS)
    .map((part, i) =>
      i % 2 === 1
        ? part
        : part.replace(FORMS, (whole, name: string | undefined) => {
            const e = name === undefined ? undefined : byName.get(name.toLowerCase());
            return e ? `<${e.animated ? "a" : ""}:${e.name}:${e.id}>` : whole;
          }),
    )
    .join("");
};
