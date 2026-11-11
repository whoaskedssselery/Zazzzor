import { Hash, Upload } from "lucide-react";
import { type DragEvent, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type Channel, computeChannel, type GuildEntry, has, PERM, useGuilds, userDisplay } from "@/entities/guild";
import {
  type AuthorView,
  attachmentsOf,
  inOrder,
  type Message,
  MessageItem,
  type PendingMessage,
  useMessages,
  usePendingMessages,
} from "@/entities/message";
import { MemberMenu } from "@/features/manage-member";
import {
  DeleteMessageDialog,
  MessageContextMenu,
  MessageEditor,
  MessageToolbar,
  useDeleteMessage,
} from "@/features/message-actions";
import {
  Composer,
  type MentionSuggestion,
  type ReplyTarget,
  useAddFiles,
  useSendMessage,
} from "@/features/send-message";
import { errorText } from "@/shared/api";
import type { MentionNames } from "@/shared/lib";
import { Button, Skeleton } from "@/shared/ui";
import { buildRows, type ChatRow } from "../model/rows";
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

const Welcome = ({ name }: { name: string }) => (
  <div className="mx-4 mt-6 mb-2">
    <span aria-hidden className="flex size-17 items-center justify-center rounded-full bg-control">
      <Hash className="size-10 text-fg" strokeWidth={1.75} />
    </span>
    <h2 className="mt-3 text-xl font-bold break-words">{`Добро пожаловать в #${name}!`}</h2>
    <p className="mt-1 text-base text-fg-muted">{`Это начало канала #${name}.`}</p>
  </div>
);

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

const DropHint = ({ channelName }: { channelName: string }) => (
  <div
    aria-hidden
    className="pointer-events-none absolute inset-2 z-(--z-sticky) flex animate-fade-in flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-accent bg-[color-mix(in_srgb,var(--bg-chat)_88%,transparent)] text-center"
  >
    <Upload className="size-10 text-accent" strokeWidth={1.5} />
    <p className="text-lg font-semibold">Отпустите, чтобы прикрепить</p>
    <p className="text-sm text-fg-muted">{`Файлы уйдут в #${channelName}`}</p>
  </div>
);

/** Names for mentions as this server shows them: members by their name here, roles with their colour. */
const mentionNames = (entry: GuildEntry): MentionNames => ({
  user: (id) => {
    const member = entry.members[id];
    return member ? { name: userDisplay(entry, member.user).name } : null;
  },
  role: (id) => {
    const role = entry.roles[id];
    return role ? { name: role.name, color: role.color } : null;
  },
});

/**
 * Who `@` can reach in `channelId`: the members who can see it, by the name
 * they go by here; the roles that may be mentioned (all of them with
 * MENTION_EVERYONE, which also brings @everyone and @here).
 */
const mentionSuggestions = (
  entry: GuildEntry,
  channelId: string,
  meId: string | null,
  everyone: boolean,
): MentionSuggestion[] => {
  const users = Object.values(entry.members)
    .filter((m) => m.user.id !== meId && has(computeChannel(entry, channelId, m.user.id), PERM.VIEW_CHANNEL))
    .map((m): MentionSuggestion => {
      const view = userDisplay(entry, m.user);
      return { kind: "user", id: m.user.id, name: view.name, detail: m.user.username, avatarUrl: view.avatarUrl };
    })
    .sort((a, b) => a.name.localeCompare(b.name, "ru"));
  const roles = Object.values(entry.roles)
    .filter((r) => r.id !== entry.guild.id && (r.mentionable || everyone))
    .map((r): MentionSuggestion => ({ kind: "role", id: r.id, name: r.name, color: r.color }));
  const groups: MentionSuggestion[] = everyone
    ? [
        { kind: "everyone", id: null, name: "everyone", detail: "Все, кто видит канал" },
        { kind: "here", id: null, name: "here", detail: "Все, кто сейчас в сети" },
      ]
    : [];
  return [...users, ...roles, ...groups];
};

/** How a message author shows here; null for a deleted account. */
const authorOf = (entry: GuildEntry, user: Message["author"]): AuthorView | null =>
  user === null ? null : userDisplay(entry, user);

/**
 * The chat of a text channel: its history (with the start of the channel at
 * the top once it is all loaded), the messages being sent, who is typing,
 * and the composer. What the user may do follows their permissions in the
 * channel: without READ_MESSAGE_HISTORY no history is asked for, without
 * SEND_MESSAGES the composer gives way to a notice, and only the author or
 * MANAGE_MESSAGES may delete. The caller keys it by channel.
 */
export const ChatView = ({ guildId, channel }: { guildId: string; channel: Channel }) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const meId = useGuilds((s) => s.me?.id ?? null);
  const perms = entry && meId !== null ? computeChannel(entry, channel.id, meId) : 0n;
  const canRead = has(perms, PERM.READ_MESSAGE_HISTORY);
  const canSend = has(perms, PERM.SEND_MESSAGES);
  const canManage = has(perms, PERM.MANAGE_MESSAGES);
  const canAttach = canSend && has(perms, PERM.ATTACH_FILES);
  const canMentionEveryone = has(perms, PERM.MENTION_EVERYONE);

  const history = useMessages(channel.id, canRead);
  const pending = usePendingMessages(channel.id);
  const sender = useSendMessage(channel.id);
  const remove = useDeleteMessage();
  const now = useMinute();
  const addFiles = useAddFiles(channel.id, canAttach);
  const drop = useFileDrop(addFiles);

  const [replyTo, setReplyTo] = useState<ReplyTarget | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<Message | null>(null);
  const [scrollTo, setScrollTo] = useState<{ id: string } | null>(null);
  const [highlighted, setHighlighted] = useState<string | null>(null);

  useEffect(() => {
    if (highlighted === null) return;
    const timer = setTimeout(() => {
      setHighlighted(null);
    }, HIGHLIGHT_MS);
    return () => {
      clearTimeout(timer);
    };
  }, [highlighted]);

  const messages = useMemo(() => (history.data ? inOrder(history.data) : []), [history.data]);
  const loaded = useMemo(() => new Set(messages.map((m) => m.id)), [messages]);
  const hasOlder = history.hasNextPage;
  const rows = useMemo(
    () => buildRows(messages, pending, { hasOlder, meId, now }),
    [messages, pending, hasOlder, meId, now],
  );

  const { fetchNextPage, isFetchingNextPage } = history;
  const loadOlder = useCallback(() => {
    void fetchNextPage();
  }, [fetchNextPage]);

  if (!entry) return null;
  const mentions = mentionNames(entry);
  const name = (u: Message["author"]) => authorOf(entry, u)?.name ?? "Удалённый пользователь";

  const onEditLast = (): boolean => {
    const last = [...messages].reverse().find((m) => m.author?.id === meId);
    if (!last) return false;
    setEditing(last.id);
    return true;
  };

  const renderMessage = (m: Message, grouped: boolean): ReactNode => {
    const own = m.author?.id === meId;
    const handlers = {
      message: m,
      canEdit: own,
      canDelete: own || canManage,
      canReply: canSend,
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
    };
    const original = m.referenced_message;
    return (
      <MessageContextMenu {...handlers}>
        <div>
          <MessageItem
            message={m}
            mentions={mentions}
            attachments={attachmentsOf(m)}
            author={authorOf(entry, m.author)}
            replyAuthor={original ? authorOf(entry, original.author) : null}
            grouped={grouped && editing !== m.id}
            now={now}
            highlighted={highlighted === m.id}
            renderName={(nameNode) => {
              const authorId = m.author?.id;
              // A member's name opens their card; a former member's is only a name.
              if (authorId === undefined || !(authorId in entry.members)) return nameNode;
              return (
                <MemberMenu guildId={guildId} userId={authorId} mode="dropdown">
                  <button type="button" className="min-w-0 cursor-pointer truncate hover:underline">
                    {nameNode}
                  </button>
                </MemberMenu>
              );
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
        return <Welcome name={channel.name} />;
      case "older":
        return <Loading label="Загружаем более ранние сообщения…" />;
      case "day":
        return <DaySeparator label={row.label} />;
      case "message":
        return renderMessage(row.message, row.grouped);
      case "pending": {
        const p = row.pending;
        const me = meId === null ? undefined : entry.members[meId]?.user;
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
            mentions={mentions}
            author={me ? userDisplay(entry, me) : null}
            replyAuthor={p.replyTo ? authorOf(entry, p.replyTo.author) : null}
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

  const label = `Сообщения в #${channel.name}`;
  let body: ReactNode;
  if (!canRead) {
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
        label={label}
        rows={rows}
        renderRow={renderRow}
        hasOlder={hasOlder}
        loadingOlder={isFetchingNextPage}
        loadOlder={loadOlder}
        scrollTo={scrollTo}
      />
    );
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col" {...(canSend ? drop.handlers : {})}>
      {body}
      <Composer
        channelId={channel.id}
        channelName={channel.name}
        canSend={canSend}
        canAttach={canAttach}
        replyTo={replyTo}
        onCancelReply={() => {
          setReplyTo(null);
        }}
        onEditLast={onEditLast}
        mentionables={() => mentionSuggestions(entry, channel.id, meId, canMentionEveryone)}
      />
      <TypingIndicator
        channelId={channel.id}
        meId={meId}
        nameOf={(userId) => {
          const member = entry.members[userId];
          return member ? userDisplay(entry, member.user).name : "Кто-то";
        }}
      />
      <DeleteMessageDialog
        message={deleting}
        onClose={() => {
          setDeleting(null);
        }}
        onConfirm={(m) => {
          remove.mutate(m);
        }}
      />
      {drop.over && <DropHint channelName={channel.name} />}
    </div>
  );
};
