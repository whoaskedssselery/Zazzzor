import { Copy, Ellipsis, Hash, Pencil, Reply, SmilePlus, Trash2 } from "lucide-react";
import { type MouseEvent, type ReactNode, useRef, useState } from "react";
import type { GuildEmoji } from "@/entities/guild";
import type { Message } from "@/entities/message";
import { useMediaQuery } from "@/shared/lib";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
  cn,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  IconButton,
  Sheet,
  SheetContent,
  useToast,
} from "@/shared/ui";
import { recentEmoji, rememberEmoji } from "../model/emoji";
import { EmojiGlyph, emojiLabel, usable } from "./EmojiGlyph";
import { EmojiPicker } from "./EmojiPicker";

/** What the user may do with one message, and what the chat does when they do. */
export interface MessageActionHandlers {
  message: Message;
  /** The author: only they edit. */
  canEdit: boolean;
  /** The author, or MANAGE_MESSAGES in the channel. */
  canDelete: boolean;
  /** READ_MESSAGE_HISTORY (a reply needs it) and SEND_MESSAGES. */
  canReply: boolean;
  onReply: () => void;
  onEdit: () => void;
  /** `skipConfirm` when Shift was held, as Discord does. */
  onDelete: (skipConfirm: boolean) => void;
  /** Puts the user's reaction (its key: `👍` or `name:id`); absent without ADD_REACTIONS. */
  onReact?: ((emoji: string) => void) | undefined;
  /** The server's custom emoji, for the picker and the quick reactions. */
  emojis?: Readonly<Record<string, GuildEmoji>> | undefined;
}

/** Copies text and says so; a browser that refuses says that instead. */
const useCopy = () => {
  const toast = useToast();
  return (text: string, done: string) => {
    navigator.clipboard.writeText(text).then(
      () => {
        toast.show({ text: done, tone: "success" });
      },
      () => {
        toast.show({ text: "Не удалось скопировать", tone: "error" });
      },
    );
  };
};

interface Item {
  key: string;
  label: string;
  icon: typeof Copy;
  danger?: boolean;
  run: (e: { shiftKey: boolean }) => void;
}

/** The actions of a message in menu order: reply, edit, copy, then delete apart. */
const useItems = ({
  message,
  canEdit,
  canDelete,
  canReply,
  onReply,
  onEdit,
  onDelete,
}: MessageActionHandlers): Item[] => {
  const copy = useCopy();
  const items: Item[] = [];
  if (canReply) items.push({ key: "reply", label: "Ответить", icon: Reply, run: onReply });
  if (canEdit) items.push({ key: "edit", label: "Изменить", icon: Pencil, run: onEdit });
  items.push(
    {
      key: "copy-text",
      label: "Копировать текст",
      icon: Copy,
      run: () => {
        copy(message.content, "Текст скопирован");
      },
    },
    {
      key: "copy-id",
      label: "Копировать ID",
      icon: Hash,
      run: () => {
        copy(message.id, "ID скопирован");
      },
    },
  );
  if (canDelete) {
    items.push({
      key: "delete",
      label: "Удалить",
      icon: Trash2,
      danger: true,
      run: (e) => {
        onDelete(e.shiftKey);
      },
    });
  }
  return items;
};

/**
 * The bar over a message on hover or focus: reply, edit and delete at
 * hand, the rest under «Ещё». Shift+click on delete skips the question.
 */
export const MessageToolbar = (props: MessageActionHandlers) => {
  const items = useItems(props);
  const quick = items.filter((i) => i.key === "reply" || i.key === "edit" || i.key === "delete");
  return (
    <div className="absolute -top-4 right-4 z-10 hidden items-center rounded-md bg-overlay p-0.5 shadow-float group-focus-within:flex group-hover:flex has-[[data-state=open]]:flex">
      {props.onReact && (
        <EmojiPicker onPick={props.onReact} emojis={props.emojis}>
          <IconButton size="sm" label="Добавить реакцию" icon={SmilePlus} />
        </EmojiPicker>
      )}
      {quick.map((i) => (
        <IconButton
          key={i.key}
          size="sm"
          label={i.label}
          icon={i.icon}
          variant={i.danger ? "danger" : "ghost"}
          onClick={(e: MouseEvent) => {
            i.run({ shiftKey: e.shiftKey });
          }}
        />
      ))}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <IconButton size="sm" label="Ещё" icon={Ellipsis} />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {items.map((i) => (
            <MenuRow key={i.key} item={i} kind="dropdown" />
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
};

const MenuRow = ({ item, kind }: { item: Item; kind: "dropdown" | "context" }) => {
  const Icon = item.icon;
  const Row = kind === "dropdown" ? DropdownMenuItem : ContextMenuItem;
  const Separator = kind === "dropdown" ? DropdownMenuSeparator : ContextMenuSeparator;
  return (
    <>
      {item.danger && <Separator />}
      <Row
        variant={item.danger ? "danger" : "default"}
        onSelect={() => {
          // From a menu, deleting always asks: Shift is for the quick button.
          item.run({ shiftKey: false });
        }}
      >
        <Icon aria-hidden className="size-4" />
        {item.label}
      </Row>
    </>
  );
};

/** Reactions at the top of the message's menu: the user's recent ones. */
const QUICK_REACTIONS = 6;

/** The user's recent emoji that can be used here, for the top of the menu. */
const quickReactions = (emojis: Readonly<Record<string, GuildEmoji>> | undefined): string[] =>
  recentEmoji()
    .filter((e) => usable(e, emojis))
    .slice(0, QUICK_REACTIONS);

/** A touch screen: there a long press opens the actions as a sheet from the bottom. */
const TOUCH = "(pointer: coarse)";

/**
 * A phone's actions of a message, as Discord's: a sheet from the bottom with
 * the quick reactions and every action as a full-width row.
 */
const ActionSheet = ({
  open,
  onOpenChange,
  items,
  onReact,
  onMore,
  onCloseAutoFocus,
  emojis,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: Item[];
  onReact: ((emoji: string) => void) | undefined;
  onMore: () => void;
  onCloseAutoFocus: (e: Event) => void;
  emojis: Readonly<Record<string, GuildEmoji>> | undefined;
}) => {
  const close = () => {
    onOpenChange(false);
  };
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="bottom"
        title="Действия с сообщением"
        className="flex-col bg-overlay"
        onCloseAutoFocus={onCloseAutoFocus}
      >
        <span aria-hidden className="mx-auto mt-2 h-1 w-10 shrink-0 rounded-full bg-border-strong" />
        <div className="overflow-y-auto px-3 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
          {onReact && (
            <div className="mb-3 flex justify-between gap-1">
              {quickReactions(emojis).map((emoji) => (
                <button
                  key={emoji}
                  type="button"
                  aria-label={`Реакция ${emojiLabel(emoji, emojis)}`}
                  className="flex size-12 cursor-pointer items-center justify-center rounded-full bg-control text-2xl leading-none"
                  onClick={() => {
                    rememberEmoji(emoji);
                    onReact(emoji);
                    close();
                  }}
                >
                  <EmojiGlyph emojiKey={emoji} emojis={emojis} />
                </button>
              ))}
              <button
                type="button"
                aria-label="Ещё реакции"
                className="flex size-12 cursor-pointer items-center justify-center rounded-full bg-control text-fg-muted"
                onClick={onMore}
              >
                <SmilePlus aria-hidden className="size-6" />
              </button>
            </div>
          )}
          <ul className="overflow-hidden rounded-lg bg-control">
            {items.map((i) => {
              const Icon = i.icon;
              return (
                <li key={i.key} className="border-b border-border last:border-b-0">
                  <button
                    type="button"
                    className={cn(
                      "flex h-12 w-full cursor-pointer items-center gap-3 px-4 text-left text-base font-medium",
                      i.danger && "text-fg-danger",
                    )}
                    onClick={() => {
                      close();
                      i.run({ shiftKey: false });
                    }}
                  >
                    <Icon aria-hidden className="size-5 shrink-0" />
                    {i.label}
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      </SheetContent>
    </Sheet>
  );
};

/**
 * The same actions on right click (or the context menu key) anywhere on the
 * message. On a touch screen, where the toolbar never shows, a long press
 * opens them as a sheet from the bottom instead: a menu at the finger would
 * run off the screen. Both start with quick reactions and «Ещё реакции»,
 * which opens the picker at the message.
 */
export const MessageContextMenu = ({ children, ...props }: MessageActionHandlers & { children: ReactNode }) => {
  const items = useItems(props);
  const react = props.onReact;
  const touch = useMediaQuery(TOUCH);
  const [sheet, setSheet] = useState(false);
  const [picking, setPicking] = useState(false);
  // «Ещё реакции» opens the picker once the menu or sheet is gone: opened
  // while it still closes, the picker would take its leaving for a click away.
  const toPicker = useRef(false);
  const afterClose = (e: Event) => {
    if (!toPicker.current) return;
    toPicker.current = false;
    e.preventDefault();
    requestAnimationFrame(() => {
      setPicking(true);
    });
  };
  const pick = (emoji: string) => {
    rememberEmoji(emoji);
    react?.(emoji);
  };
  const picker = react && (
    <EmojiPicker open={picking} onOpenChange={setPicking} onPick={react} emojis={props.emojis}>
      {/* Where the picker opens from the menu: the message's top right, as its toolbar's button. */}
      <span aria-hidden tabIndex={-1} className="pointer-events-none absolute top-0 right-16 size-px" />
    </EmojiPicker>
  );

  if (touch) {
    return (
      // biome-ignore lint/a11y/noStaticElementInteractions: a long press of the message; the same actions are in its toolbar and keyboard menu.
      // biome-ignore lint/a11y/noNoninteractiveElementInteractions: as above.
      <div
        className="relative"
        // A long press on a phone fires contextmenu; the browser's own menu has nothing for a message.
        onContextMenu={(e) => {
          if (e.target instanceof Element && e.target.closest("a, img, video, input, textarea")) return;
          e.preventDefault();
          setSheet(true);
        }}
      >
        {children}
        <ActionSheet
          open={sheet}
          onOpenChange={setSheet}
          items={items}
          onReact={react}
          onMore={() => {
            toPicker.current = true;
            setSheet(false);
          }}
          onCloseAutoFocus={afterClose}
          emojis={props.emojis}
        />
        {picker}
      </div>
    );
  }

  return (
    <div className="relative">
      <ContextMenu>
        <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
        <ContextMenuContent
          // Its natural width: shrunk at the screen's edge, it would be cut instead of moved in.
          className="w-max"
          onCloseAutoFocus={afterClose}
        >
          {react && (
            <>
              <ContextMenuGroup aria-label="Быстрые реакции" className="flex gap-0.5 pb-1">
                {quickReactions(props.emojis).map((emoji) => (
                  <ContextMenuItem
                    key={emoji}
                    aria-label={`Реакция ${emojiLabel(emoji, props.emojis)}`}
                    className="size-9 justify-center px-0 text-xl leading-none"
                    onSelect={() => {
                      pick(emoji);
                    }}
                  >
                    <EmojiGlyph emojiKey={emoji} emojis={props.emojis} />
                  </ContextMenuItem>
                ))}
              </ContextMenuGroup>
              <ContextMenuItem
                onSelect={() => {
                  toPicker.current = true;
                }}
              >
                Ещё реакции
                <SmilePlus aria-hidden />
              </ContextMenuItem>
              <ContextMenuSeparator />
            </>
          )}
          {items.map((i) => (
            <MenuRow key={i.key} item={i} kind="context" />
          ))}
        </ContextMenuContent>
      </ContextMenu>
      {picker}
    </div>
  );
};
