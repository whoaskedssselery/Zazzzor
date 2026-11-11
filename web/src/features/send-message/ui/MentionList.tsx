import { AtSign } from "lucide-react";
import { Avatar, cn, roleTextStyle } from "@/shared/ui";
import type { MentionSuggestion } from "../model/mentions";

export const MENTION_LIST_ID = "composer-mentions";
export const optionId = (i: number) => `${MENTION_LIST_ID}-${String(i)}`;

/**
 * Who `@` can reach, over the composer: members with their avatar and
 * username, roles in their colour, then @everyone and @here. The field keeps
 * the focus and drives it (↑/↓, Enter or Tab, Esc); a click picks too.
 */
export const MentionList = ({
  suggestions,
  active,
  onPick,
  onHover,
}: {
  suggestions: readonly MentionSuggestion[];
  active: number;
  onPick: (s: MentionSuggestion) => void;
  onHover: (i: number) => void;
}) => (
  <div className="absolute inset-x-0 bottom-full mb-2 animate-float overflow-hidden rounded-lg bg-overlay p-1.5 shadow-float ring-1 ring-border">
    <p className="px-2 pt-1 pb-1.5 text-xs font-semibold text-fg-faint">Упомянуть</p>
    <div id={MENTION_LIST_ID} role="listbox" aria-label="Кого упомянуть">
      {suggestions.map((s, i) => (
        // biome-ignore lint/a11y/useKeyWithClickEvents: the field drives the list through aria-activedescendant; the keys are handled there.
        <div
          key={`${s.kind}:${s.id ?? s.name}`}
          id={optionId(i)}
          role="option"
          tabIndex={-1}
          aria-selected={i === active}
          className={cn(
            "flex h-10 cursor-pointer items-center gap-2.5 rounded-md px-2 text-base",
            i === active && "bg-hover",
          )}
          // The field keeps the focus: a mousedown would take it.
          onMouseDown={(e) => {
            e.preventDefault();
          }}
          onMouseEnter={() => {
            onHover(i);
          }}
          onClick={() => {
            onPick(s);
          }}
        >
          {s.kind === "user" ? (
            <Avatar name={s.name} src={s.avatarUrl} size={24} />
          ) : (
            <span
              aria-hidden
              className="role-text flex size-6 shrink-0 items-center justify-center"
              style={roleTextStyle(s.color ?? 0)}
            >
              <AtSign className="size-4.5" />
            </span>
          )}
          <bdi
            className={cn("truncate font-medium", s.kind === "role" && "role-text")}
            style={s.kind === "role" ? roleTextStyle(s.color ?? 0) : undefined}
          >
            {s.kind === "role" || s.kind === "user" ? s.name : `@${s.name}`}
          </bdi>
          {s.detail && <span className="ml-auto truncate text-sm text-fg-faint">{s.detail}</span>}
        </div>
      ))}
    </div>
  </div>
);
