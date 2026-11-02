import { type KeyboardEvent, useLayoutEffect, useRef, useState } from "react";
import type { Message } from "@/entities/message";
import { runeLength, trimContent } from "@/shared/lib";
import { cn } from "@/shared/ui";
import { useEditMessage } from "../model/mutations";

const MAX_LENGTH = 4000;

/**
 * A message's text being edited in place. Enter saves, Shift+Enter breaks
 * the line, Esc leaves it as it was. The same text again closes without a
 * request; an empty one cannot be saved (deleting is its own action).
 */
export const MessageEditor = ({ message, onDone }: { message: Message; onDone: () => void }) => {
  const [text, setText] = useState(message.content);
  const edit = useEditMessage();
  const field = useRef<HTMLTextAreaElement>(null);

  useLayoutEffect(() => {
    const el = field.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${String(Math.min(el.scrollHeight, 320))}px`;
  }, [text]);

  // Opens with the caret at the end, like Discord.
  useLayoutEffect(() => {
    const el = field.current;
    if (!el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }, []);

  const content = trimContent(text);
  const invalid = content === "" || runeLength(content) > MAX_LENGTH;

  const save = () => {
    if (invalid) return;
    if (content !== message.content) edit.mutate({ message, content });
    onDone();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      save();
    } else if (e.key === "Escape") {
      e.preventDefault();
      // The chat closes nothing else on this Esc.
      e.stopPropagation();
      onDone();
    }
  };

  return (
    <div className="py-1">
      <textarea
        ref={field}
        rows={1}
        value={text}
        aria-label="Изменить сообщение"
        aria-invalid={invalid || undefined}
        className={cn(
          "max-h-80 w-full resize-none rounded-lg bg-input px-4 py-2.5 text-base text-fg outline-none",
          invalid && "ring-1 ring-danger",
        )}
        onChange={(e) => {
          setText(e.target.value);
        }}
        onKeyDown={onKeyDown}
      />
      <p className="mt-1 text-xs text-fg-muted">
        Esc —{" "}
        <button type="button" className="cursor-pointer text-link hover:underline" onClick={onDone}>
          отмена
        </button>
        {" • "}Enter —{" "}
        <button
          type="button"
          className="cursor-pointer text-link hover:underline disabled:cursor-not-allowed disabled:opacity-50"
          disabled={invalid}
          onClick={save}
        >
          сохранить
        </button>
      </p>
    </div>
  );
};
