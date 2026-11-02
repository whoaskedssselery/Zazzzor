import { SendHorizontal } from "lucide-react";
import { type KeyboardEvent, useLayoutEffect, useRef } from "react";
import type { MessageStub } from "@/entities/message";
import { runeLength, trimContent } from "@/shared/lib";
import { cn, IconButton } from "@/shared/ui";
import { getDraft, setDraft, useDraft } from "../model/drafts";
import { useSendMessage } from "../model/useSendMessage";
import { useTyping } from "../model/useTyping";
import { ReplyBar } from "./ReplyBar";

/** The server's limit, in code points after trimming. */
export const MAX_LENGTH = 4000;
/** The counter shows from this length on. */
const COUNTER_FROM = 3800;
/** The field grows with its text up to this height, then scrolls. */
const MAX_HEIGHT_PX = 320;

export const NO_SEND_PERMISSION = "У вас нет прав на отправку сообщений в этом канале";

/** The message being answered and the name its author goes by here. */
export interface ReplyTarget {
  message: MessageStub;
  name: string;
}

export interface ComposerProps {
  channelId: string;
  channelName: string;
  /** SEND_MESSAGES in the channel; without it the field gives way to a notice. */
  canSend: boolean;
  replyTo: ReplyTarget | null;
  onCancelReply: () => void;
  /** ↑ in the empty field: edit the user's last message here; false when there is none. */
  onEditLast: () => boolean;
}

/**
 * The message field of a channel. Enter sends, Shift+Enter breaks the line.
 * The text is the channel's draft: it stays when the user switches channels
 * or the connection drops. One Enter (or click) sends one message: the text
 * is read from the draft store and cleared in the same event, so a second
 * Enter before the next render finds nothing to send.
 */
export const Composer = ({ channelId, channelName, canSend, replyTo, onCancelReply, onEditLast }: ComposerProps) => {
  const text = useDraft(channelId);
  const { send } = useSendMessage(channelId);
  const typing = useTyping(channelId);
  const field = useRef<HTMLTextAreaElement>(null);

  // Grows with its text, then scrolls.
  useLayoutEffect(() => {
    const el = field.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${String(Math.min(el.scrollHeight, MAX_HEIGHT_PX))}px`;
  }, [text]);

  if (!canSend) {
    return (
      <div className="shrink-0 px-4 pb-6">
        <p className="flex min-h-11 items-center rounded-lg bg-input px-4 text-base text-fg-muted">
          {NO_SEND_PERMISSION}
        </p>
      </div>
    );
  }

  // Only a long text can be over the limit; counting code points is not free.
  const length = text.length > COUNTER_FROM ? runeLength(trimContent(text)) : text.length;
  const tooLong = length > MAX_LENGTH;
  const empty = trimContent(text) === "";

  const submit = () => {
    const content = trimContent(getDraft(channelId));
    if (content === "" || runeLength(content) > MAX_LENGTH) return;
    setDraft(channelId, "");
    send(content, replyTo?.message ?? null);
    typing.sent();
    onCancelReply();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      submit();
    } else if (e.key === "ArrowUp" && e.currentTarget.value === "" && onEditLast()) {
      e.preventDefault();
    } else if (e.key === "Escape" && replyTo) {
      e.preventDefault();
      onCancelReply();
    }
  };

  return (
    <div className="shrink-0 px-4 pb-6">
      {replyTo && <ReplyBar name={replyTo.name} onCancel={onCancelReply} />}
      <div className={cn("flex items-end gap-1 bg-input pr-2 pl-4", replyTo ? "rounded-b-lg" : "rounded-lg")}>
        <textarea
          ref={field}
          rows={1}
          value={text}
          aria-label={`Сообщение в #${channelName}`}
          placeholder={`Написать в #${channelName}`}
          aria-invalid={tooLong || undefined}
          className="max-h-80 min-h-11 min-w-0 flex-1 resize-none bg-transparent py-2.5 text-base text-fg outline-none"
          onChange={(e) => {
            setDraft(channelId, e.target.value);
            if (e.target.value !== "") typing.typed();
          }}
          onKeyDown={onKeyDown}
        />
        {length > COUNTER_FROM && (
          <output
            aria-label="Осталось символов"
            className={cn("mb-3 shrink-0 text-xs tabular-nums", tooLong ? "text-fg-danger" : "text-fg-muted")}
          >
            {MAX_LENGTH - length}
          </output>
        )}
        <IconButton
          className="mb-1.5"
          label="Отправить"
          icon={SendHorizontal}
          disabled={empty || tooLong}
          onClick={submit}
        />
      </div>
    </div>
  );
};
