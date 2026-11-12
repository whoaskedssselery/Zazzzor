import { CirclePlus, SendHorizontal } from "lucide-react";
import { type ClipboardEvent, type KeyboardEvent, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { MessageStub } from "@/entities/message";
import { runeLength, trimContent } from "@/shared/lib";
import { cn, IconButton, useToast } from "@/shared/ui";
import { getDraft, setDraft, useDraft } from "../model/drafts";
import { type ComposerEmoji, emojiQuery, encodeEmoji, rankEmoji } from "../model/emoji";
import {
  encodeMentions,
  type MentionSuggestion,
  mentionQuery,
  rankSuggestions,
  rememberMention,
} from "../model/mentions";
import { addFiles, isUploading, NO_ATTACH_PERMISSION, takeUploads, useUploads } from "../model/uploads";
import { useSendMessage } from "../model/useSendMessage";
import { useTyping } from "../model/useTyping";
import { EmojiList } from "./EmojiList";
import { MENTION_LIST_ID, MentionList, optionId } from "./MentionList";
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
  /** The field's name, as a screen reader says it: «Сообщение в #общий», «Сообщение для @Аня». */
  label: string;
  /** What the empty field says: «Написать в #общий», «Написать @Аня». */
  placeholder: string;
  /** SEND_MESSAGES in the channel; without it the field gives way to a notice. */
  canSend: boolean;
  /** ATTACH_FILES in the channel: files may be added (button, paste, drop). */
  canAttach: boolean;
  replyTo: ReplyTarget | null;
  onCancelReply: () => void;
  /** ↑ in the empty field: edit the user's last message here; false when there is none. */
  onEditLast: () => boolean;
  /** Who `@` can mention here, asked for when the user types one; none by default. */
  mentionables?: () => readonly MentionSuggestion[];
  /** The server's custom emoji, for `:name` and for sending `:name:`; none by default. */
  emojis?: () => readonly ComposerEmoji[];
}

/** An `@query` or `:query` being typed: its trigger, where it starts and what follows. */
type Query = { kind: "@" | ":"; start: number; query: string };

/** The query at the caret of `el`, when nothing is selected. */
const queryAt = (el: HTMLTextAreaElement): Query | null => {
  const caret = el.selectionStart;
  if (caret !== el.selectionEnd) return null;
  const at = mentionQuery(el.value, caret);
  if (at) return { kind: "@", ...at };
  const colon = emojiQuery(el.value, caret);
  return colon ? { kind: ":", ...colon } : null;
};

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
  label,
  placeholder,
  canSend,
  canAttach,
  replyTo,
  onCancelReply,
  onEditLast,
  mentionables,
  emojis,
}: ComposerProps) => {
  const text = useDraft(channelId);
  const uploads = useUploads(channelId);
  const add = useAddFiles(channelId, canAttach);
  const picker = useRef<HTMLInputElement>(null);
  const { send } = useSendMessage(channelId);
  const typing = useTyping(channelId);
  const field = useRef<HTMLTextAreaElement>(null);
  // The @query or :query at the caret and the highlighted suggestion; closed (null) by Esc until the caret moves on.
  const [mention, setMention] = useState<Query | null>(null);
  const [active, setActive] = useState(0);
  const suggestions = useMemo(
    () => (mention?.kind === "@" && mentionables ? rankSuggestions(mentionables(), mention.query) : []),
    [mention, mentionables],
  );
  const emojiSuggestions = useMemo(
    () => (mention?.kind === ":" && emojis ? rankEmoji(emojis(), mention.query) : []),
    [mention, emojis],
  );
  const count = suggestions.length + emojiSuggestions.length;
  const listOpen = count > 0;

  /** Looks for an @query or :query at the caret after the text or the caret changed. */
  const track = (el: HTMLTextAreaElement) => {
    const next = queryAt(el);
    setMention((prev) =>
      prev?.kind === next?.kind && prev?.start === next?.start && prev?.query === next?.query ? prev : next,
    );
    if (next?.query !== mention?.query) setActive(0);
  };

  /** Puts `insert` in place of the query at the caret and keeps the caret after it. */
  const replaceQuery = (insert: string) => {
    const el = field.current;
    if (!el || !mention) return;
    const value = el.value;
    const caret = el.selectionStart;
    setDraft(channelId, value.slice(0, mention.start) + insert + value.slice(caret));
    setMention(null);
    const at = mention.start + insert.length;
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(at, at);
    });
  };

  /** Puts `@Name ` in place of the @query and remembers what it means. */
  const pick = (s: MentionSuggestion) => {
    replaceQuery(`@${s.name} `);
    rememberMention(channelId, s);
  };

  /** Puts `:name: ` in place of the :query; it becomes the emoji when sent. */
  const pickEmoji = (e: ComposerEmoji) => {
    replaceQuery(`:${e.name}: `);
  };

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
    const content = encodeEmoji(encodeMentions(channelId, trimContent(getDraft(channelId))), emojis?.() ?? []);
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
    if (listOpen) {
      const n = count;
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setActive((i) => (i + (e.key === "ArrowDown" ? 1 : n - 1)) % n);
        return;
      }
      if ((e.key === "Enter" || e.key === "Tab") && !e.shiftKey && !e.nativeEvent.isComposing) {
        e.preventDefault();
        const s = suggestions[active];
        const emoji = emojiSuggestions[active];
        if (s) pick(s);
        else if (emoji) pickEmoji(emoji);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setMention(null);
        return;
      }
    }
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
    <div className="relative shrink-0 px-4 pb-6">
      {listOpen && (
        <div className="relative">
          {suggestions.length > 0 ? (
            <MentionList suggestions={suggestions} active={active} onPick={pick} onHover={setActive} />
          ) : (
            <EmojiList
              query={mention?.query ?? ""}
              suggestions={emojiSuggestions}
              active={active}
              onPick={pickEmoji}
              onHover={setActive}
            />
          )}
        </div>
      )}
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
            aria-label={label}
            placeholder={placeholder}
            aria-invalid={tooLong || undefined}
            aria-autocomplete="list"
            aria-controls={listOpen ? MENTION_LIST_ID : undefined}
            aria-activedescendant={listOpen ? optionId(active) : undefined}
            className={cn(
              "max-h-80 min-h-11 min-w-0 flex-1 resize-none bg-transparent py-2.5 text-base text-fg outline-none",
              !canAttach && "pl-2",
            )}
            onChange={(e) => {
              setDraft(channelId, e.target.value);
              if (e.target.value !== "") typing.typed();
              track(e.target);
            }}
            onSelect={(e) => {
              track(e.currentTarget);
            }}
            onBlur={() => {
              setMention(null);
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
