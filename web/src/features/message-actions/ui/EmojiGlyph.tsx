import type { GuildEmoji } from "@/entities/guild";
import { emojiOfKey } from "@/entities/message";
import { cn } from "@/shared/ui";

/**
 * An emoji by its key (`👍` or `name:id`): Unicode as text, a custom one of
 * this server as its picture, sized by `className` (`size-*` for the
 * picture, `text-*` for the text). A custom one not here (deleted, or of
 * another server) shows as its `:name:`.
 */
export const EmojiGlyph = ({
  emojiKey,
  emojis,
  className,
}: {
  emojiKey: string;
  emojis: Readonly<Record<string, GuildEmoji>> | undefined;
  className?: string;
}) => {
  const e = emojiOfKey(emojiKey);
  if (e.id === null) return <span className={cn("leading-none", className)}>{e.name}</span>;
  const custom = emojis?.[e.id];
  if (!custom) return <span className="text-sm leading-none font-semibold">{`:${e.name}:`}</span>;
  return (
    <img
      src={custom.url}
      alt={`:${custom.name}:`}
      draggable={false}
      className={cn("size-[1.2em] object-contain", className)}
    />
  );
};

/** What an emoji is called: a Unicode one is itself, a custom one `:name:`. */
export const emojiLabel = (emojiKey: string, emojis?: Readonly<Record<string, GuildEmoji>>): string => {
  const e = emojiOfKey(emojiKey);
  if (e.id === null) return e.name;
  return `:${emojis?.[e.id]?.name ?? e.name}:`;
};

/** Whether the user may use emoji `key` here: any Unicode one, a custom one only of this server. */
export const usable = (key: string, emojis?: Readonly<Record<string, GuildEmoji>>): boolean => {
  const e = emojiOfKey(key);
  return e.id === null || (emojis !== undefined && e.id in emojis);
};
