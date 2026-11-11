import { useQuery } from "@tanstack/react-query";
import { SmilePlus } from "lucide-react";
import { type Message, type Reaction, reactionsOf } from "@/entities/message";
import { api, type components, unwrap } from "@/shared/api";
import { cn, Tooltip } from "@/shared/ui";
import { EmojiPicker } from "./EmojiPicker";

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
}: {
  message: Message;
  reaction: Reaction;
  nameOf: (user: User) => string;
}) => {
  const emoji = reaction.emoji.name;
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
      <span className="text-2xl leading-none">{emoji}</span>
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
}: {
  message: Message;
  canAdd: boolean;
  onToggle: (emoji: string, add: boolean) => void;
  nameOf: (user: User) => string;
}) => {
  const reactions = reactionsOf(message);
  if (reactions.length === 0) return null;
  return (
    <ul aria-label="Реакции" className="mt-1 flex flex-wrap items-center gap-1">
      {reactions.map((r) => (
        <li key={r.emoji.name}>
          <Tooltip content={<Reactors message={message} reaction={r} nameOf={nameOf} />}>
            <button
              type="button"
              aria-pressed={r.me}
              aria-label={`${r.emoji.name} ${String(r.count)}`}
              className={cn(
                "flex h-7 cursor-pointer items-center gap-1.5 rounded-md px-2 text-sm font-semibold tabular-nums ring-1 transition-colors duration-(--duration-fast)",
                r.me
                  ? "bg-accent/15 text-fg ring-accent hover:bg-accent/25"
                  : "bg-control text-fg-muted ring-transparent hover:ring-border-strong",
              )}
              onClick={() => {
                onToggle(r.emoji.name, !r.me);
              }}
            >
              <span className="text-lg leading-none">{r.emoji.name}</span>
              {r.count}
            </button>
          </Tooltip>
        </li>
      ))}
      {canAdd && (
        <li className="opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 has-[[data-state=open]]:opacity-100">
          <EmojiPicker
            onPick={(emoji) => {
              const held = reactions.find((r) => r.emoji.name === emoji);
              if (!held?.me) onToggle(emoji, true);
            }}
          >
            <button
              type="button"
              aria-label="Добавить реакцию"
              className="flex h-7 cursor-pointer items-center rounded-md bg-control px-2 text-fg-muted transition-colors hover:text-fg"
            >
              <SmilePlus aria-hidden className="size-4.5" />
            </button>
          </EmojiPicker>
        </li>
      )}
    </ul>
  );
};
