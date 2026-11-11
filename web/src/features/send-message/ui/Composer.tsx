import { CirclePlus, SendHorizontal } from "lucide-react";
import { type ClipboardEvent, type KeyboardEvent, useLayoutEffect, useRef } from "react";
import type { MessageStub } from "@/entities/message";
import { runeLength, trimContent } from "@/shared/lib";
import { cn, IconButton, useToast } from "@/shared/ui";
import { getDraft, setDraft, useDraft } from "../model/drafts";
import { addFiles, isUploading, NO_ATTACH_PERMISSION, takeUploads, useUploads } from "../model/uploads";
import { useSendMessage } from "../model/useSendMessage";
import { useTyping } from "../model/useTyping";
import { ReplyBar } from "./ReplyBar";
import { UploadTray } from "./UploadTray";

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
  /** ATTACH_FILES in the channel: files may be added (button, paste, drop). */
  canAttach: boolean;
  replyTo: ReplyTarget | null;
  onCancelReply: () => void;
  /** ↑ in the empty field: edit the user's last message here; false when there is none. */
  onEditLast: () => boolean;
}

/** Says why files were left out, if any were. */
export const useAddFiles = (channelId: string, canAttach: boolean) => {
  const toast = useToast();
  return (files: readonly File[]) => {
    if (files.length === 0) return;
    const problem = canAttach ? addFiles(channelId, files) : NO_ATTACH_PERMISSION;
    if (problem !== null) toast.show({ text: problem, tone: "error" });
  };
};

/**
 * The message field of a channel. Enter sends, Shift+Enter breaks the line.
 * Files come from the button, a paste or a drop on the chat; each uploads
 * at once with its progress, and the message goes once none is uploading.
 * A message may be files alone.
 * The text is the channel's draft: it stays when the user switches channels
 * or the connection drops. One Enter (or click) sends one message: the text
 * is read from the draft store and cleared in the same event, so a second
 * Enter before the next render finds nothing to send.
 */
export const Composer = ({
  channelId,
  channelName,
  canSend,
  canAttach,
  replyTo,
  onCancelReply,
  onEditLast,
}: ComposerProps) => {
  const text = useDraft(channelId);
  const uploads = useUploads(channelId);
  const add = useAddFiles(channelId, canAttach);
  const picker = useRef<HTMLInputElement>(null);
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
  const uploading = isUploading(uploads);
  const ready = uploads.some((u) => u.status === "done");
  const empty = trimContent(text) === "" && !ready;

  const submit = () => {
    if (uploading) return;
    const content = trimContent(getDraft(channelId));
    if (runeLength(content) > MAX_LENGTH) return;
    const files = takeUploads(channelId);
    if (content === "" && files.length === 0) return;
    setDraft(channelId, "");
    send(content, replyTo?.message ?? null, files);
    typing.sent();
    onCancelReply();
  };

  // A screenshot pasted from the clipboard becomes a file; text pastes as usual.
  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = [...e.clipboardData.files];
    if (files.length === 0) return;
    e.preventDefault();
    add(files);
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
      <div className={cn("bg-input", replyTo ? "rounded-b-lg" : "rounded-lg")}>
        <UploadTray channelId={channelId} uploads={uploads} />
        <div className="flex items-end gap-1 pr-2 pl-2">
          {canAttach && (
            <>
              <IconButton
                className="mb-1.5 shrink-0"
                label="Прикрепить файлы"
                icon={CirclePlus}
                onClick={() => picker.current?.click()}
              />
              <input
                ref={picker}
                type="file"
                multiple
                hidden
                tabIndex={-1}
                onChange={(e) => {
                  add([...(e.target.files ?? [])]);
                  e.target.value = "";
                }}
              />
            </>
          )}
          <textarea
            ref={field}
            rows={1}
            value={text}
            aria-label={`Сообщение в #${channelName}`}
            placeholder={`Написать в #${channelName}`}
            aria-invalid={tooLong || undefined}
            className={cn(
              "max-h-80 min-h-11 min-w-0 flex-1 resize-none bg-transparent py-2.5 text-base text-fg outline-none",
              !canAttach && "pl-2",
            )}
            onChange={(e) => {
              setDraft(channelId, e.target.value);
              if (e.target.value !== "") typing.typed();
            }}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
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
            label={uploading ? "Файлы ещё загружаются" : "Отправить"}
            icon={SendHorizontal}
            disabled={empty || tooLong || uploading}
            onClick={submit}
          />
        </div>
      </div>
    </div>
  );
};
