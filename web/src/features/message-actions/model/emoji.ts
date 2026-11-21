import { t, textTable } from "@/shared/i18n";
/**
 * The emoji of the reaction picker: Unicode's, with Russian names and
 * keywords (emojibase), loaded the first time the picker opens.
 *
 * A reaction is its exact string: the server compares bytes, so 👍 with and
 * without U+FE0F would be two reactions. Every emoji the client sends comes
 * from this list (or from a reaction already on the message), in emojibase's
 * form, so everyone's 👍 is one.
 */

export interface PickerEmoji {
  /** Its key: the Unicode emoji, or `name:id` for a custom one. */
  emoji: string;
  /** A custom emoji's picture. */
  url?: string;
  label: string;
  /** The label and keywords, lower-cased, for search. */
  terms: string;
}

export interface EmojiGroup {
  name: string;
  emoji: PickerEmoji[];
}

/** emojibase's groups as the picker names them; 2 (skin tone parts) is not offered. */
const GROUPS: Readonly<Record<number, string>> = textTable({
  0: () => t("chat:emojiGroups.0"),
  1: () => t("chat:emojiGroups.1"),
  3: () => t("chat:emojiGroups.3"),
  4: () => t("chat:emojiGroups.4"),
  5: () => t("chat:emojiGroups.5"),
  6: () => t("chat:emojiGroups.6"),
  7: () => t("chat:emojiGroups.7"),
  8: () => t("chat:emojiGroups.8"),
  9: () => t("chat:emojiGroups.9"),
});

/**
 * The newest Unicode version offered: Windows 11 draws up to 15.0, newer
 * emoji would show as empty boxes there.
 */
const MAX_VERSION = 15;

interface Raw {
  emoji: string;
  label: string;
  tags?: string[];
  group?: number;
  order?: number;
  version: number;
}

let loading: Promise<EmojiGroup[]> | null = null;

/** The picker's emoji by group, loaded once. */
export const loadEmoji = (): Promise<EmojiGroup[]> => {
  loading ??= import("emojibase-data/ru/data.json")
    .then((m) => groupEmoji(m.default as Raw[]))
    .catch((e: unknown) => {
      loading = null;
      throw e;
    });
  return loading;
};

export const groupEmoji = (raw: readonly Raw[]): EmojiGroup[] => {
  const groups = new Map<number, PickerEmoji[]>();
  const sorted = [...raw].sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  for (const e of sorted) {
    if (e.group === undefined || !(e.group in GROUPS) || e.version > MAX_VERSION) continue;
    const list = groups.get(e.group) ?? [];
    list.push({
      emoji: e.emoji,
      label: e.label,
      terms: [e.label, ...(e.tags ?? [])].join(" ").toLocaleLowerCase("ru"),
    });
    groups.set(e.group, list);
  }
  return [...groups.entries()]
    .sort(([a], [b]) => a - b)
    .map(([group, emoji]) => ({ name: GROUPS[group] ?? "", emoji }));
};

/** Emoji whose name or keywords hold every word of `query`. */
export const searchEmoji = (groups: readonly EmojiGroup[], query: string): PickerEmoji[] => {
  const words = query.toLocaleLowerCase("ru").replaceAll(":", " ").split(/\s+/).filter(Boolean);
  if (words.length === 0) return [];
  return groups.flatMap((g) => g.emoji).filter((e) => words.every((w) => e.terms.includes(w)));
};

/** A server's custom emoji as the picker offers them. */
export const customPickerEmoji = (e: { id: string; name: string; url: string }): PickerEmoji => ({
  emoji: `${e.name}:${e.id}`,
  label: `:${e.name}:`,
  terms: e.name.toLowerCase(),
  url: e.url,
});

const RECENT_KEY = "zazzzor.recentEmoji";
const MAX_RECENT = 16;
/** Before the user has picked any: 👍 ❤️ 😂 😮 😢 🔥 🎉 👀, in emojibase's form. */
export const DEFAULT_RECENT = [
  "\u{1F44D}\u{FE0F}",
  "\u{2764}\u{FE0F}",
  "\u{1F602}",
  "\u{1F62E}",
  "\u{1F622}",
  "\u{1F525}",
  "\u{1F389}",
  "\u{1F440}",
];

/** The emoji the user picked lately, newest first. */
export const recentEmoji = (): string[] => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(RECENT_KEY) ?? "null");
    if (Array.isArray(raw) && raw.every((x) => typeof x === "string") && raw.length > 0)
      return raw.slice(0, MAX_RECENT);
  } catch {
    // Storage blocked or spoiled: the defaults do.
  }
  return DEFAULT_RECENT;
};

/** Puts `emoji` first among the recent ones. */
export const rememberEmoji = (emoji: string): void => {
  const next = [emoji, ...recentEmoji().filter((e) => e !== emoji)].slice(0, MAX_RECENT);
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(next));
  } catch {
    // Not remembered; nothing else depends on it.
  }
};
