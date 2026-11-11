import { CornerUpLeft } from "lucide-react";
import type { ReactNode } from "react";
import { formatMessageTime, Markdown, type MentionNames, markdownText } from "@/shared/lib";
import { Avatar, cn, roleTextStyle, Tooltip } from "@/shared/ui";
import type { Attachment } from "../model/attachment";
import type { Message } from "../model/cache";
import { Attachments } from "./Attachments";
import { type Embed, Embeds } from "./Embeds";

/** How a user shows in this server: their nickname (or display name), role colour and avatar. */
export interface AuthorView {
  name: string;
  /** The colour of their highest coloured role; 0 for none. */
  color: number;
  avatarUrl: string | null;
}

export const DELETED_USER = "Удалённый пользователь";
export const REPLY_DELETED = "Исходное сообщение удалено";

const pad = (n: number) => String(n).padStart(2, "0");
/** «21:04», the time beside a message that continues a group. */
const clock = (d: Date) => `${pad(d.getHours())}:${pad(d.getMinutes())}`;

export interface MessageItemProps {
  message: Pick<Message, "id" | "content" | "created_at" | "edited_at" | "reply_to_id" | "referenced_message">;
  /** null: the author's account is gone. */
  author: AuthorView | null;
  /** The author of the answered message, when there is one and it still exists. */
  replyAuthor: AuthorView | null;
  /** Continues the group of the message above: no avatar or name, the time on hover. */
  grouped: boolean;
  now: Date;
  /** A message of this client not confirmed yet, or one that failed. */
  state?: "sending" | "failed";
  /** Shown instead of the text while the message is edited. */
  editor?: ReactNode;
  /** Names for the mentions in its text (and in the reply preview). */
  mentions?: MentionNames;
  /** The files of the message, under its text. */
  attachments?: Attachment[];
  /** Link previews, under the attachments. */
  embeds?: readonly Embed[];
  /** Under the text (the actions of a failed message). */
  footer?: ReactNode;
  /** The reactions under the message. */
  reactions?: ReactNode;
  /** The bar of actions shown on hover or focus. */
  toolbar?: ReactNode;
  /** Scrolls to the answered message; absent when it is not loaded. */
  onJumpToReply?: (() => void) | undefined;
  /** Briefly marked after a jump to it. */
  highlighted?: boolean;
  /** Mentions the reader: marked for as long as it is shown. */
  mentioned?: boolean;
  /** Wraps the author's name (a button that opens their card); the name alone by default. */
  renderName?: (name: ReactNode) => ReactNode;
}

/** The preview above a reply: who and what it answers, or that the original is gone. */
const ReplyPreview = ({
  message,
  author,
  onJump,
  mentions,
}: {
  message: MessageItemProps["message"];
  author: AuthorView | null;
  onJump: (() => void) | undefined;
  mentions: MentionNames | undefined;
}) => {
  const original = message.referenced_message;
  const body =
    original === null ? (
      <span className="truncate italic">{REPLY_DELETED}</span>
    ) : (
      <>
        <Avatar name={author?.name ?? DELETED_USER} src={author?.avatarUrl} size={20} />
        <bdi className="role-text shrink-0 font-semibold" style={roleTextStyle(author?.color ?? 0)}>
          @{author?.name ?? DELETED_USER}
        </bdi>
        <span className="truncate">{markdownText(original.content, mentions)}</span>
      </>
    );
  return (
    <div className="relative mb-1 flex min-w-0 items-center gap-1.5 text-sm text-fg-muted">
      <span
        aria-hidden
        className="absolute top-2.5 -left-9 h-5 w-7.5 rounded-tl-md border-t-2 border-l-2 border-border-strong"
      />
      {original !== null && onJump ? (
        <button
          type="button"
          aria-label="Перейти к исходному сообщению"
          className="flex min-w-0 cursor-pointer items-center gap-1.5 hover:text-fg"
          onClick={onJump}
        >
          {body}
        </button>
      ) : (
        <span className="flex min-w-0 items-center gap-1.5">
          {original === null && <CornerUpLeft aria-hidden className="size-4 shrink-0" />}
          {body}
        </span>
      )}
    </div>
  );
};

/**
 * One message of the chat, laid out as Discord does: the first of a group
 * with avatar, name and time; the ones continuing it with only their text
 * and the time on hover. A reply shows what it answers above. Names sit in
 * <bdi>, so right-to-left text in a name cannot reorder the line around it.
 */
export const MessageItem = ({
  message,
  author,
  replyAuthor,
  grouped,
  now,
  state,
  editor,
  mentions,
  attachments = [],
  embeds = [],
  footer,
  reactions,
  toolbar,
  onJumpToReply,
  highlighted = false,
  mentioned = false,
  renderName = (n) => n,
}: MessageItemProps) => {
  const sent = new Date(message.created_at);
  const name = author?.name ?? DELETED_USER;
  const reply = message.reply_to_id !== null;
  const edited = message.edited_at === null ? null : new Date(message.edited_at);
  const body = editor ?? (
    <div className={cn("text-base", state === "sending" && "text-fg-muted", state === "failed" && "text-fg-danger")}>
      <Markdown
        source={message.content}
        {...(mentions ? { mentions } : {})}
        after={
          edited && (
            <Tooltip content={formatMessageTime(edited, now)}>
              <button type="button" className="ml-1 cursor-default align-baseline text-xs text-fg-faint">
                (изменено)
              </button>
            </Tooltip>
          )
        }
      />
    </div>
  );
  return (
    <div
      data-message-id={message.id}
      className={cn(
        "group relative py-0.5 pr-12 pl-18 transition-colors hover:bg-message-hover focus-within:bg-message-hover",
        !grouped && "mt-4",
        (highlighted || mentioned) && "bg-mention hover:bg-mention",
        mentioned && "shadow-[inset_2px_0_0_var(--mention-bar)]",
      )}
    >
      {grouped ? (
        <time
          dateTime={message.created_at}
          className="absolute left-0 w-18 pt-1 text-center text-[11px] text-fg-faint opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
        >
          {clock(sent)}
        </time>
      ) : (
        <>
          {reply && <ReplyPreview message={message} author={replyAuthor} onJump={onJumpToReply} mentions={mentions} />}
          <div className={cn("absolute left-4", reply ? "top-7.5" : "top-1")}>
            <Avatar name={name} src={author?.avatarUrl} size={40} />
          </div>
          <h3 className="flex min-w-0 items-baseline gap-2">
            {renderName(
              <bdi
                className={cn("truncate text-base font-semibold", author ? "role-text" : "text-fg-muted")}
                style={author ? roleTextStyle(author.color) : undefined}
              >
                {name}
              </bdi>,
            )}
            <time dateTime={message.created_at} className="shrink-0 text-xs text-fg-faint">
              {formatMessageTime(sent, now)}
            </time>
          </h3>
        </>
      )}
      {body}
      <Attachments attachments={attachments} />
      <Embeds embeds={embeds} />
      {reactions}
      {footer}
      {toolbar}
    </div>
  );
};
