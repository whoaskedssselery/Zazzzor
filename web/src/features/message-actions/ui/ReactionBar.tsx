import { useQuery } from "@tanstack/react-query";
import { SmilePlus } from "lucide-react";
import type { GuildEmoji } from "@/entities/guild";
import { emojiKey, emojiOfKey, type Message, type Reaction, reactionsOf, sameEmoji } from "@/entities/message";
import { api, type components, unwrap } from "@/shared/api";
import { cn, Tooltip } from "@/shared/ui";
import { EmojiGlyph, emojiLabel } from "./EmojiGlyph";
import { EmojiPicker } from "./EmojiPicker";

type Emojis = Readonly<Record<string, GuildEmoji>>;

type User = components["schemas"]["User"];

/** Names shown in a reaction's tooltip before «и ещё N». */
const NAMED = 3;

/** «Аня, Пётр и ещё 2» from the first users of a reaction and its count. */
export const reactorsText = (names: string[], count: number): string => {
  const shown = names.slice(0, NAMED);
  const rest = count - shown.length;
  if (shown.length === 0) return String(count);
  if (rest <= 0)
    return shown.length === 1 ? (shown[0] ?? "") : `${shown.slice(0, -1).join(", ")} и ${shown.at(-1) ?? ""}`;
  return `${shown.join(", ")} и ещё ${String(rest)}`;
};

/** Who put a reaction, asked for when its tooltip opens. */
const Reactors = ({
  message,
  reaction,
  nameOf,
  emojis,
}: {
  message: Message;
  reaction: Reaction;
  nameOf: (user: User) => string;
  emojis: Emojis | undefined;
}) => {
  const emoji = emojiKey(reaction.emoji);
  const { data } = useQuery({
    queryKey: ["reactors", message.id, emoji, reaction.count],
    queryFn: async () =>
      unwrap(
        await api.GET("/channels/{channel_id}/messages/{message_id}/reactions/{emoji}", {
          params: { path: { channel_id: message.channel_id, message_id: message.id, emoji }, query: { limit: NAMED } },
        }),
      ),
    staleTime: 30_000,
  });
  return (
    <span className="flex items-center gap-2">
      <EmojiGlyph emojiKey={emoji} emojis={emojis} className="text-2xl" />
      <span>{data ? reactorsText(data.map(nameOf), reaction.count) : "…"}</span>
    </span>
  );
};

/**
 * The reactions under a message, as Discord shows them: a chip per emoji
 * with its count, marked when one is the user's; a click adds or takes
 * back theirs. Hovering a chip tells who reacted. A new emoji needs
 * ADD_REACTIONS (`canAdd`); joining one already there does not.
 */
export const ReactionBar = ({
  message,
  canAdd,
  onToggle,
  nameOf,
  emojis,
}: {
  message: Message;
  canAdd: boolean;
  /** `emoji` is its key: the Unicode emoji, or `name:id` for a custom one. */
  onToggle: (emoji: string, add: boolean) => void;
  nameOf: (user: User) => string;
  /** The server's custom emoji; a reaction with one not here shows its `:name:`. */
  emojis?: Emojis;
}) => {
  const reactions = reactionsOf(message);
  if (reactions.length === 0) return null;
  return (
    <ul aria-label="Реакции" className="mt-1 flex flex-wrap items-center gap-1">
      {reactions.map((r) => (
        <li key={r.emoji.id ?? r.emoji.name}>
          <Tooltip content={<Reactors message={message} reaction={r} nameOf={nameOf} emojis={emojis} />}>
            <button
              type="button"
              aria-pressed={r.me}
              aria-label={`${emojiLabel(emojiKey(r.emoji), emojis)} ${String(r.count)}`}
              className={cn(
                "flex h-7 cursor-pointer items-center gap-1.5 rounded-md px-2 text-sm font-semibold tabular-nums ring-1 pointer-coarse:h-9 pointer-coarse:px-2.5",
                "transition-[color,background-color,box-shadow,scale] duration-(--duration-fast) active:scale-95",
                r.me
                  ? "bg-accent/15 text-fg ring-accent hover:bg-accent/25"
                  : "bg-control text-fg-muted ring-transparent hover:ring-border-strong",
              )}
              onClick={() => {
                onToggle(emojiKey(r.emoji), !r.me);
              }}
            >
              <EmojiGlyph emojiKey={emojiKey(r.emoji)} emojis={emojis} className="text-lg" />
              {r.count}
            </button>
          </Tooltip>
        </li>
      ))}
      {canAdd && (
        <li className="opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 has-[[data-state=open]]:opacity-100">
          <EmojiPicker
            emojis={emojis}
            onPick={(emoji) => {
              const held = reactions.find((r) => sameEmoji(r.emoji, emojiOfKey(emoji)));
              if (!held?.me) onToggle(emoji, true);
            }}
          >
            <button
              type="button"
              aria-label="Добавить реакцию"
              className="flex h-7 cursor-pointer items-center rounded-md bg-control px-2 text-fg-muted transition-[color,scale] duration-(--duration-fast) hover:text-fg active:scale-95 pointer-coarse:h-9 pointer-coarse:px-2.5"
            >
              <SmilePlus aria-hidden className="size-4.5" />
            </button>
          </EmojiPicker>
        </li>
      )}
    </ul>
  );
};
