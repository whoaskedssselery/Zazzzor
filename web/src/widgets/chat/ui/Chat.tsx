import { Upload } from "lucide-react";
import { type DragEvent, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getGuildsState, unreadFrom, useGuilds } from "@/entities/guild";
import {
  attachmentsOf,
  emojiOfKey,
  inOrder,
  type Message,
  MessageItem,
  type PendingMessage,
  reactionsOf,
  sameEmoji,
  useMessages,
  usePendingMessages,
} from "@/entities/message";
import {
  DeleteMessageDialog,
  MessageContextMenu,
  MessageEditor,
  MessageToolbar,
  ReactionBar,
  useDeleteMessage,
  useToggleReaction,
} from "@/features/message-actions";
import { useReadChannel } from "@/features/read-channel";
import { Composer, type ReplyTarget, useAddFiles, useSendMessage } from "@/features/send-message";
import { errorText } from "@/shared/api";
import { Button, Skeleton } from "@/shared/ui";
import { buildRows, type ChatRow } from "../model/rows";
import type { ChatScope } from "../model/scope";
import { MessageList } from "./MessageList";
import { TypingIndicator } from "./TypingIndicator";

export const NO_HISTORY = "У вас нет доступа к истории этого канала";
/** How long a message stays marked after a jump to it. */
const HIGHLIGHT_MS = 2_000;

/** The current time, renewed every minute (for «Сегодня» and grouping of new messages). */
const useMinute = (): Date => {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = setInterval(() => {
      setNow(new Date());
    }, 60_000);
    return () => {
      clearInterval(timer);
    };
  }, []);
  return now;
};

/** Placeholder rows while older history loads. */
const Loading = ({ label }: { label: string }) => (
  <div className="flex flex-col gap-4 px-4 pt-6" aria-busy>
    <span className="sr-only">{label}</span>
    {[0, 1].map((i) => (
      <div key={i} aria-hidden className="flex gap-4">
        <Skeleton className="size-10 shrink-0 rounded-full" />
        <div className="flex flex-1 flex-col gap-2 pt-1">
          <Skeleton className="h-4 w-32 rounded-sm" />
          <Skeleton className="h-4 w-3/4 rounded-sm" />
        </div>
      </div>
    ))}
  </div>
);

/** Where the unread messages start, as Discord marks it: a red line with «Новые» at its end. */
const NewDivider = () => (
  <div className="mx-4 mt-6 mb-1 flex items-center">
    <span className="sr-only">Новые сообщения</span>
    <span aria-hidden className="h-px flex-1 bg-danger" />
    <span
      aria-hidden
      className="rounded-sm bg-danger px-1 text-[10px] leading-4 font-bold tracking-wide text-on-danger uppercase"
    >
      Новые
    </span>
  </div>
);

const DaySeparator = ({ label }: { label: string }) => (
  <div className="mx-4 mt-6 mb-1 flex items-center gap-2 text-xs font-semibold text-fg-faint">
    <span aria-hidden className="h-px flex-1 bg-border" />
    {label}
    <span aria-hidden className="h-px flex-1 bg-border" />
  </div>
);

/** The words of a failed send and its way out: send again, or drop it. */
const FailedFooter = ({
  pending,
  onRetry,
  onDiscard,
}: {
  pending: PendingMessage;
  onRetry: () => void;
  onDiscard: () => void;
}) => (
  <p className="mt-0.5 text-xs text-fg-danger">
    {`Не отправлено: ${pending.error ?? "ошибка"}. `}
    <button type="button" className="cursor-pointer font-semibold text-link hover:underline" onClick={onRetry}>
      Повторить
    </button>
    {" · "}
    <button type="button" className="cursor-pointer font-semibold text-link hover:underline" onClick={onDiscard}>
      Удалить
    </button>
  </p>
);

/** A message of someone the user blocked, folded as Discord folds it, with a way to read it anyway. */
const BlockedMessage = ({ onShow }: { onShow: () => void }) => (
  <p className="mx-4 my-1 py-1 pl-14 text-sm text-fg-faint">
    Сообщение от заблокированного пользователя.{" "}
    <button type="button" className="cursor-pointer font-medium text-link hover:underline" onClick={onShow}>
      Показать
    </button>
  </p>
);

/** Whether a drag carries files (not text or a link). */
const carriesFiles = (e: DragEvent) => e.dataTransfer.types.includes("Files");

/**
 * Files dragged over the chat attach to the message being written: the whole
 * chat is the drop target, with a hint over it while files hover.
 */
const useFileDrop = (onFiles: (files: File[]) => void) => {
  const [over, setOver] = useState(false);
  // dragenter and dragleave fire for every child crossed; a count tells the
  // real leaving from moving between children.
  const depth = useRef(0);
  return {
    over,
    handlers: {
      onDragEnter: (e: DragEvent) => {
        if (!carriesFiles(e)) return;
        e.preventDefault();
        depth.current += 1;
        setOver(true);
      },
      onDragOver: (e: DragEvent) => {
        if (!carriesFiles(e)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
      },
      onDragLeave: (e: DragEvent) => {
        if (!carriesFiles(e)) return;
        depth.current = Math.max(0, depth.current - 1);
        if (depth.current === 0) setOver(false);
      },
      onDrop: (e: DragEvent) => {
        if (!carriesFiles(e)) return;
        e.preventDefault();
        depth.current = 0;
        setOver(false);
        onFiles([...e.dataTransfer.files]);
      },
    },
  };
};

const DropHint = ({ text }: { text: string }) => (
  <div
    aria-hidden
    className="pointer-events-none absolute inset-2 z-(--z-sticky) flex animate-fade-in flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-accent bg-[color-mix(in_srgb,var(--bg-chat)_88%,transparent)] text-center"
  >
    <Upload className="size-10 text-accent" strokeWidth={1.5} />
    <p className="text-lg font-semibold">Отпустите, чтобы прикрепить</p>
    <p className="text-sm text-fg-muted">{text}</p>
  </div>
);

/**
 * A chat: its history (with the start at the top once it is all loaded),
 * the messages being sent, who is typing, and the composer. Where it is
 * and what the user may do there come from `scope` (a server channel or a
 * private one). Only the author, or someone with `can.manage`, deletes a
 * message. The caller keys it by channel.
 */
export const Chat = ({ scope }: { scope: ChatScope }) => {
  const { channelId, can } = scope;
  const meId = useGuilds((s) => s.me?.id ?? null);
  const me = useGuilds((s) => s.me);

  const history = useMessages(channelId, can.read);
  const pending = usePendingMessages(channelId);
  const sender = useSendMessage(channelId);
  const remove = useDeleteMessage();
  const react = useToggleReaction();
  const now = useMinute();
  const addFiles = useAddFiles(channelId, can.attach);
  const drop = useFileDrop(addFiles);

  const [replyTo, setReplyTo] = useState<ReplyTarget | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<Message | null>(null);
  const [scrollTo, setScrollTo] = useState<{ id: string } | null>(null);
  const [highlighted, setHighlighted] = useState<string | null>(null);
  const [shown, setShown] = useState<ReadonlySet<string>>(() => new Set());
  const [atBottom, setAtBottom] = useState(true);
  // Fixed while the channel stays open, so the line does not move as the user reads.
  const [readFrom] = useState(() => unreadFrom(getGuildsState(), channelId));

  useEffect(() => {
    if (highlighted === null) return;
    const timer = setTimeout(() => {
      setHighlighted(null);
    }, HIGHLIGHT_MS);
    return () => {
      clearTimeout(timer);
    };
  }, [highlighted]);

  // Read only once the history shows: until then there is nothing to have read.
  useReadChannel(channelId, history.isSuccess && atBottom);

  const messages = useMemo(() => (history.data ? inOrder(history.data) : []), [history.data]);
  const loaded = useMemo(() => new Set(messages.map((m) => m.id)), [messages]);
  const hasOlder = history.hasNextPage;
  const rows = useMemo(
    () => buildRows(messages, pending, { hasOlder, meId, now, readFrom }),
    [messages, pending, hasOlder, meId, now, readFrom],
  );

  const { fetchNextPage, isFetchingNextPage } = history;
  const loadOlder = useCallback(() => {
    void fetchNextPage();
  }, [fetchNextPage]);

  const authorOf = (user: Message["author"]) => (user === null ? null : scope.author(user));
  const name = (u: Message["author"]) => authorOf(u)?.name ?? "Удалённый пользователь";

  const onEditLast = (): boolean => {
    const last = [...messages].reverse().find((m) => m.author?.id === meId);
    if (!last) return false;
    setEditing(last.id);
    return true;
  };

  const renderMessage = (m: Message, grouped: boolean): ReactNode => {
    if (scope.hidden(m) && !shown.has(m.id)) {
      return (
        <BlockedMessage
          onShow={() => {
            setShown((s) => new Set(s).add(m.id));
          }}
        />
      );
    }
    const own = m.author?.id === meId;
    const handlers = {
      message: m,
      canEdit: own,
      canDelete: own || can.manage,
      canReply: can.send,
      onReply: () => {
        setReplyTo({ message: { id: m.id, author: m.author, content: m.content }, name: name(m.author) });
      },
      onEdit: () => {
        setEditing(m.id);
      },
      onDelete: (skipConfirm: boolean) => {
        if (skipConfirm) remove.mutate(m);
        else setDeleting(m);
      },
      onReact: can.react
        ? (emoji: string) => {
            // Picking one already the user's leaves it, as in Discord.
            const picked = emojiOfKey(emoji);
            if (!reactionsOf(m).some((r) => sameEmoji(r.emoji, picked) && r.me))
              react.mutate({ message: m, emoji, add: true });
          }
        : undefined,
      emojis: scope.emojis,
    };
    const original = m.referenced_message;
    return (
      <MessageContextMenu {...handlers}>
        <div>
          <MessageItem
            message={m}
            mentions={scope.mentions}
            attachments={attachmentsOf(m)}
            embeds={m.embeds}
            reactions={
              <ReactionBar
                message={m}
                canAdd={can.react}
                onToggle={(emoji, add) => {
                  react.mutate({ message: m, emoji, add });
                }}
                nameOf={(u) => scope.author(u).name}
                emojis={scope.emojis}
              />
            }
            author={authorOf(m.author)}
            replyAuthor={original ? authorOf(original.author) : null}
            grouped={grouped && editing !== m.id}
            now={now}
            highlighted={highlighted === m.id}
            mentioned={!own && scope.mentioned(m)}
            renderName={(nameNode) => {
              const authorId = m.author?.id;
              return authorId === undefined ? nameNode : scope.nameMenu(authorId, nameNode);
            }}
            editor={
              editing === m.id ? (
                <MessageEditor
                  message={m}
                  onDone={() => {
                    setEditing(null);
                  }}
                />
              ) : undefined
            }
            toolbar={editing === m.id ? undefined : <MessageToolbar {...handlers} />}
            onJumpToReply={
              m.reply_to_id !== null && loaded.has(m.reply_to_id)
                ? () => {
                    const id = m.reply_to_id;
                    if (id === null) return;
                    setScrollTo({ id });
                    setHighlighted(id);
                  }
                : undefined
            }
          />
        </div>
      </MessageContextMenu>
    );
  };

  const renderRow = (row: ChatRow): ReactNode => {
    switch (row.kind) {
      case "start":
        return scope.welcome;
      case "older":
        return <Loading label="Загружаем более ранние сообщения…" />;
      case "day":
        return <DaySeparator label={row.label} />;
      case "new":
        return <NewDivider />;
      case "message":
        return renderMessage(row.message, row.grouped);
      case "pending": {
        const p = row.pending;
        return (
          <MessageItem
            message={{
              id: p.nonce,
              content: p.content,
              created_at: now.toISOString(),
              edited_at: null,
              reply_to_id: p.replyTo?.id ?? null,
              referenced_message: p.replyTo,
            }}
            attachments={p.attachments}
            mentions={scope.mentions}
            author={me ? scope.author(me) : null}
            replyAuthor={p.replyTo ? authorOf(p.replyTo.author) : null}
            grouped={row.grouped}
            now={now}
            state={p.status}
            footer={
              p.status === "failed" ? (
                <FailedFooter
                  pending={p}
                  onRetry={() => {
                    sender.retry(p.nonce);
                  }}
                  onDiscard={() => {
                    sender.discard(p.nonce);
                  }}
                />
              ) : undefined
            }
          />
        );
      }
    }
  };

  let body: ReactNode;
  if (!can.read) {
    body = (
      <div className="flex min-h-0 flex-1 items-end">
        <p className="mx-4 mb-4 text-base text-fg-muted">{NO_HISTORY}</p>
      </div>
    );
  } else if (history.isPending) {
    body = (
      <div className="flex min-h-0 flex-1 flex-col justify-end">
        <Loading label="Загружаем сообщения…" />
      </div>
    );
  } else if (history.isError) {
    body = (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
        <p className="text-base text-fg-muted">{errorText(history.error)}</p>
        <Button
          variant="secondary"
          onClick={() => {
            void history.refetch();
          }}
        >
          Повторить
        </Button>
      </div>
    );
  } else {
    body = (
      <MessageList
        label={scope.listLabel}
        rows={rows}
        renderRow={renderRow}
        hasOlder={hasOlder}
        loadingOlder={isFetchingNextPage}
        loadOlder={loadOlder}
        scrollTo={scrollTo}
        onAtBottom={setAtBottom}
      />
    );
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col" {...(can.send ? drop.handlers : {})}>
      {body}
      <Composer
        channelId={channelId}
        label={scope.composerLabel}
        placeholder={scope.composerPlaceholder}
        canSend={can.send}
        canAttach={can.attach}
        replyTo={replyTo}
        onCancelReply={() => {
          setReplyTo(null);
        }}
        onEditLast={onEditLast}
        mentionables={scope.mentionables}
        emojis={scope.composerEmojis}
      />
      <TypingIndicator channelId={channelId} meId={meId} nameOf={scope.typingName} />
      <DeleteMessageDialog
        message={deleting}
        onClose={() => {
          setDeleting(null);
        }}
        onConfirm={(m) => {
          remove.mutate(m);
        }}
      />
      {drop.over && <DropHint text={scope.dropHint} />}
    </div>
  );
};
