import { type CSSProperties, createContext, type ReactNode, use, useState } from "react";
import { type MdNode, type MentionKind, parseMarkdown } from "./parse";

/** Who or what a mention names, as this server shows them. */
export interface MentionTarget {
  name: string;
  /** A role's colour (0 or absent for none). */
  color?: number;
}

/** Looks up the names of mentions; null for one that is not here (a former member, a deleted role). */
export interface MentionNames {
  user: (id: string) => MentionTarget | null;
  role: (id: string) => MentionTarget | null;
}

const NO_NAMES: MentionNames = { user: () => null, role: () => null };
const MentionContext = createContext<MentionNames>(NO_NAMES);

export const UNKNOWN_USER = "неизвестный";
export const DELETED_ROLE = "удалённая роль";

/** The text of a mention: `@name`, `@everyone`, `@here`. */
export const mentionText = (names: MentionNames, kind: MentionKind, id: string | null): string => {
  switch (kind) {
    case "everyone":
    case "here":
      return `@${kind}`;
    case "user":
      return `@${(id !== null && names.user(id)?.name) || UNKNOWN_USER}`;
    case "role":
      return `@${(id !== null && names.role(id)?.name) || DELETED_ROLE}`;
  }
};

const hex = (color: number) => `#${color.toString(16).padStart(6, "0")}`;

/** A mention as a tinted chip; a role takes its own colour, as in Discord. */
const Mention = ({ kind, id }: { kind: MentionKind; id: string | null }) => {
  const names = use(MentionContext);
  const color = kind === "role" && id !== null ? (names.role(id)?.color ?? 0) : 0;
  const style: CSSProperties | undefined =
    color > 0
      ? ({ "--role": hex(color), backgroundColor: `color-mix(in srgb, ${hex(color)} 18%, transparent)` } as CSSProperties)
      : undefined;
  return (
    <span
      data-mention={kind}
      className={
        color > 0
          ? "role-text rounded-xs px-0.5 font-medium"
          : "rounded-xs bg-mention-chip px-0.5 font-medium text-fg-mention"
      }
      style={style}
    >
      {mentionText(names, kind, id)}
    </span>
  );
};

/** A hidden part of a message until it is clicked (or Enter/Space). Revealed, it is plain text again. */
const Spoiler = ({ children }: { children: ReactNode }) => {
  const [shown, setShown] = useState(false);
  if (shown) return <span className="rounded-xs bg-spoiler-shown px-0.5">{children}</span>;
  return (
    <button
      type="button"
      aria-label="Спойлер, нажмите, чтобы показать"
      className="inline cursor-pointer rounded-xs bg-spoiler px-0.5 text-left text-transparent select-none [&_*]:invisible"
      onClick={() => {
        setShown(true);
      }}
    >
      {children}
    </button>
  );
};

const renderNodes = (nodes: MdNode[]): ReactNode[] => nodes.map((n, i) => renderNode(n, i));

const renderNode = (node: MdNode, key: number): ReactNode => {
  switch (node.type) {
    case "text":
      return node.value;
    case "bold":
      return <strong key={key}>{renderNodes(node.children)}</strong>;
    case "italic":
      return <em key={key}>{renderNodes(node.children)}</em>;
    case "underline":
      return <u key={key}>{renderNodes(node.children)}</u>;
    case "strike":
      return <s key={key}>{renderNodes(node.children)}</s>;
    case "spoiler":
      return <Spoiler key={key}>{renderNodes(node.children)}</Spoiler>;
    case "code":
      return (
        <code key={key} className="rounded-xs bg-code px-1 py-px font-mono text-[0.85em]">
          {node.value}
        </code>
      );
    case "codeBlock":
      return (
        <pre
          key={key}
          className="my-1 max-w-full overflow-x-auto rounded-sm border border-border bg-code p-2 font-mono text-sm whitespace-pre"
        >
          <code {...(node.lang === null ? {} : { "data-lang": node.lang })}>{node.value}</code>
        </pre>
      );
    case "quote":
      return (
        <blockquote key={key} className="my-0.5 border-l-4 border-border-strong pl-3">
          {renderNodes(node.children)}
        </blockquote>
      );
    case "mention":
      return <Mention key={key} kind={node.kind} id={node.id} />;
    case "link":
      return (
        <a
          key={key}
          href={node.href}
          target="_blank"
          rel="noopener noreferrer nofollow"
          title={node.href}
          className="text-link hover:underline"
        >
          {renderNodes(node.children)}
        </a>
      );
  }
};

/** React elements of a parsed message; never HTML. */
export const renderMarkdown = (nodes: MdNode[]): ReactNode => renderNodes(nodes);

/**
 * A message's text with its markdown; mentions show by `mentions`. Line breaks are kept and any string
 * wraps, however long, so nothing widens the chat. `after` follows the
 * last line inline (the «(изменено)» mark).
 */
export const Markdown = ({
  source,
  after,
  mentions = NO_NAMES,
}: {
  source: string;
  after?: ReactNode;
  /** Names for the mentions in the text. */
  mentions?: MentionNames;
}) => (
  <div className="break-words whitespace-pre-wrap [overflow-wrap:anywhere]">
    <MentionContext value={mentions}>{renderMarkdown(parseMarkdown(source))}</MentionContext>
    {after}
  </div>
);
