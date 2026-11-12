import { cn } from "@/shared/ui";
import type { ComposerEmoji } from "../model/emoji";
import { MENTION_LIST_ID, optionId } from "./MentionList";

/**
 * The server's emoji that match `:query`, over the composer. The field
 * keeps the focus and drives it (↑/↓, Enter or Tab, Esc); a click picks too.
 */
export const EmojiList = ({
  query,
  suggestions,
  active,
  onPick,
  onHover,
}: {
  query: string;
  suggestions: readonly ComposerEmoji[];
  active: number;
  onPick: (e: ComposerEmoji) => void;
  onHover: (i: number) => void;
}) => (
  <div className="absolute inset-x-0 bottom-full mb-2 animate-float overflow-hidden rounded-lg bg-overlay p-1.5 shadow-float ring-1 ring-border">
    <p className="truncate px-2 pt-1 pb-1.5 text-xs font-semibold text-fg-faint">{`Эмодзи по запросу :${query}`}</p>
    <div id={MENTION_LIST_ID} role="listbox" aria-label="Эмодзи">
      {suggestions.map((e, i) => (
        // biome-ignore lint/a11y/useKeyWithClickEvents: the field drives the list through aria-activedescendant; the keys are handled there.
        <div
          key={e.id}
          id={optionId(i)}
          role="option"
          tabIndex={-1}
          aria-selected={i === active}
          className={cn(
            "flex h-10 cursor-pointer items-center gap-2.5 rounded-md px-2 text-base",
            i === active && "bg-hover",
          )}
          // The field keeps the focus: a mousedown would take it.
          onMouseDown={(ev) => {
            ev.preventDefault();
          }}
          onMouseEnter={() => {
            onHover(i);
          }}
          onClick={() => {
            onPick(e);
          }}
        >
          <img src={e.url} alt="" draggable={false} className="size-6 shrink-0 object-contain" />
          <span className="truncate font-medium">{`:${e.name}:`}</span>
        </div>
      ))}
    </div>
  </div>
);
