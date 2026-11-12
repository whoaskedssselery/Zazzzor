import { Search } from "lucide-react";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import type { GuildEmoji } from "@/entities/guild";
import { cn, Popover, PopoverContent, PopoverTrigger, Spinner } from "@/shared/ui";
import {
  customPickerEmoji,
  type EmojiGroup,
  loadEmoji,
  type PickerEmoji,
  recentEmoji,
  rememberEmoji,
  searchEmoji,
} from "../model/emoji";

/** The emoji the user's pointer or focus is on, for the line at the bottom. */
type Hover = PickerEmoji | null;

/** An emoji of the picker: a custom one's picture, or the Unicode glyph. */
const Glyph = ({ e, className }: { e: PickerEmoji; className?: string }) =>
  e.url === undefined ? (
    <span className={cn("leading-none", className)}>{e.emoji}</span>
  ) : (
    <img
      src={e.url}
      alt=""
      draggable={false}
      loading="lazy"
      className={cn("size-[1.15em] object-contain", className)}
    />
  );

const Cell = ({
  e,
  onPick,
  onHover,
}: {
  e: PickerEmoji;
  onPick: (emoji: string) => void;
  onHover: (h: Hover) => void;
}) => (
  <button
    type="button"
    aria-label={e.label}
    className="flex size-10 cursor-pointer items-center justify-center rounded-md text-[28px] leading-none transition-colors duration-(--duration-fast) hover:bg-hover focus-visible:bg-hover"
    onClick={() => {
      onPick(e.emoji);
    }}
    onMouseEnter={() => {
      onHover(e);
    }}
    onFocus={() => {
      onHover(e);
    }}
  >
    <Glyph e={e} />
  </button>
);

const Section = ({
  title,
  emoji,
  onPick,
  onHover,
}: {
  title: string;
  emoji: PickerEmoji[];
  onPick: (emoji: string) => void;
  onHover: (h: Hover) => void;
}) => (
  <section aria-label={title} className="[content-visibility:auto] [contain-intrinsic-size:auto_200px]">
    <h3 className="sticky top-0 z-1 bg-overlay px-1 pt-2 pb-1 text-xs font-semibold text-fg-faint">{title}</h3>
    <div className="grid grid-cols-8">
      {emoji.map((e) => (
        <Cell key={e.emoji} e={e} onPick={onPick} onHover={onHover} />
      ))}
    </div>
  </section>
);

/** The key's ID when it names a custom emoji. */
const customId = (key: string): string | null => /:([0-9]+)$/.exec(key)?.[1] ?? null;

/**
 * Recent emoji with their names from the loaded list (a recent one no
 * longer offered keeps its glyph as its name). A custom one shows only on
 * its server, by its current name; another server's, or a deleted one, is left out.
 */
const recentOf = (groups: EmojiGroup[], custom: readonly PickerEmoji[]): PickerEmoji[] => {
  const all = new Map(groups.flatMap((g) => g.emoji).map((e) => [e.emoji, e]));
  const byId = new Map(custom.map((e) => [customId(e.emoji), e]));
  return recentEmoji().flatMap((emoji) => {
    const id = customId(emoji);
    if (id === null) return [all.get(emoji) ?? { emoji, label: emoji, terms: "" }];
    const found = byId.get(id);
    return found ? [found] : [];
  });
};

/**
 * The reaction picker, as Discord's: search by name in Russian, the recent
 * ones first, then the server's own emoji, then Unicode's groups. Enter in
 * the search picks the first match. The list loads the first time it opens.
 * A pick is the emoji's key: the Unicode emoji, or `name:id` for a custom one.
 */
export const EmojiPicker = ({
  onPick,
  children,
  open,
  onOpenChange,
  emojis,
}: {
  onPick: (emoji: string) => void;
  /** The server's custom emoji. */
  emojis?: Readonly<Record<string, GuildEmoji>> | undefined;
  /** The trigger. */
  children: ReactNode;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) => {
  const [inner, setInner] = useState(false);
  const isOpen = open ?? inner;
  const setOpen = (v: boolean) => {
    setInner(v);
    onOpenChange?.(v);
  };
  const [groups, setGroups] = useState<EmojiGroup[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [query, setQuery] = useState("");
  const [hover, setHover] = useState<Hover>(null);
  const custom = useMemo(() => Object.values(emojis ?? {}).map(customPickerEmoji), [emojis]);

  useEffect(() => {
    if (!isOpen || groups !== null) return;
    let live = true;
    loadEmoji().then(
      (g) => {
        if (live) setGroups(g);
      },
      () => {
        if (live) setFailed(true);
      },
    );
    return () => {
      live = false;
    };
  }, [isOpen, groups]);

  const pick = (emoji: string) => {
    rememberEmoji(emoji);
    onPick(emoji);
    setOpen(false);
    setQuery("");
  };
  const found =
    groups && query.trim() !== ""
      ? [...searchEmoji([{ name: "", emoji: custom }], query), ...searchEmoji(groups, query)]
      : null;

  let body: ReactNode;
  if (failed) body = <p className="p-6 text-center text-sm text-fg-muted">Не удалось загрузить эмодзи.</p>;
  else if (groups === null)
    body = (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    );
  else if (found !== null)
    body =
      found.length === 0 ? (
        <p className="p-6 text-center text-sm text-fg-muted">Ничего не нашлось.</p>
      ) : (
        <Section title="Найдено" emoji={found} onPick={pick} onHover={setHover} />
      );
  else
    body = (
      <>
        <Section title="Недавние" emoji={recentOf(groups, custom)} onPick={pick} onHover={setHover} />
        {custom.length > 0 && <Section title="Этого сервера" emoji={custom} onPick={pick} onHover={setHover} />}
        {groups.map((g) => (
          <Section key={g.name} title={g.name} emoji={g.emoji} onPick={pick} onHover={setHover} />
        ))}
      </>
    );

  return (
    <Popover
      open={isOpen}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setQuery("");
      }}
    >
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent
        align="end"
        aria-label="Выбор реакции"
        className="flex h-110 w-[calc(8*2.5rem+1.5rem)] max-w-[calc(100vw-16px)] flex-col"
      >
        <div className="relative m-2 mb-0">
          <Search
            aria-hidden
            className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-fg-faint"
          />
          <input
            // biome-ignore lint/a11y/noAutofocus: the picker opens to search, as Discord's does.
            autoFocus
            type="search"
            aria-label="Найти эмодзи"
            placeholder="Найти эмодзи"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
            }}
            onKeyDown={(e) => {
              const first = found?.[0];
              if (e.key === "Enter" && first) {
                e.preventDefault();
                pick(first.emoji);
              }
            }}
            className="h-9 w-full rounded-md bg-input pr-2 pl-8 text-sm text-fg outline-none placeholder:text-fg-faint focus-visible:ring-2 focus-visible:ring-focus"
          />
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">{body}</div>
        <div
          aria-hidden
          className={cn(
            "flex h-11 shrink-0 items-center gap-2 border-t border-border px-3 text-sm font-medium",
            hover === null && "text-fg-faint",
          )}
        >
          {hover ? (
            <>
              <Glyph e={hover} className="text-2xl" />
              <span className="truncate">{hover.label}</span>
            </>
          ) : (
            "Выберите реакцию"
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
};
