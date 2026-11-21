import { type CSSProperties, createContext, type ReactNode, use, useState } from "react";
import { t } from "@/shared/i18n";
import { type MdNode, type MentionKind, parseMarkdown } from "./parse";

/** Who or what a mention names, as this server shows them. */
export interface MentionTarget {
  name: string;
  /** A role's colour (0 or absent for none). */
  color?: number;
}

/**
 * Looks up the names of mentions; null for one that is not here (a former
 * member, a deleted role). `emoji` finds a custom emoji of this server:
 * one of another server, or deleted, shows as its `:name:`.
 */
export interface MentionNames {
  user: (id: string) => MentionTarget | null;
  role: (id: string) => MentionTarget | null;
  emoji?: (id: string) => { url: string; name?: string } | null;
}

const NO_NAMES: MentionNames = { user: () => null, role: () => null };
const MentionContext = createContext<MentionNames>(NO_NAMES);

/** The text of a mention: `@name`, `@everyone`, `@here`. */
export const mentionText = (names: MentionNames, kind: MentionKind, id: string | null): string => {
  switch (kind) {
    case "everyone":
    case "here":
      return `@${kind}`;
    case "user":
      return `@${(id !== null && names.user(id)?.name) || t("chat:unknownUser")}`;
    case "role":
      return `@${(id !== null && names.role(id)?.name) || t("chat:deletedRole")}`;
  }
};

const hex = (color: number) => `#${color.toString(16).padStart(6, "0")}`;

/** A mention as a tinted chip; a role takes its own colour, as in Discord. */
const Mention = ({ kind, id }: { kind: MentionKind; id: string | null }) => {
  const names = use(MentionContext);
  const color = kind === "role" && id !== null ? (names.role(id)?.color ?? 0) : 0;
  const style: CSSProperties | undefined =
    color > 0
      ? ({
          "--role": hex(color),
          backgroundColor: `color-mix(in srgb, ${hex(color)} 18%, transparent)`,
        } as CSSProperties)
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
      aria-label={t("chat:spoiler")}
      className="inline cursor-pointer rounded-xs bg-spoiler px-0.5 text-left text-transparent select-none [&_*]:invisible"
      onClick={() => {
        setShown(true);
      }}
    >
      {children}
    </button>
  );
};

/** A message of only custom emoji (and spaces) shows them large, as Discord does, up to this many. */
const JUMBO_MAX = 30;

/** Whether `nodes` are only custom emoji and spaces, few enough to show large. */
export const isJumbo = (nodes: MdNode[]): boolean => {
  let count = 0;
  for (const n of nodes) {
    if (n.type === "emoji") count++;
    else if (n.type !== "text" || n.value.trim() !== "") return false;
  }
  return count > 0 && count <= JUMBO_MAX;
};

const JumboContext = createContext(false);

/** A custom emoji of this server as its picture; any other as its `:name:`. */
const CustomEmoji = ({ name, id }: { name: string; id: string }) => {
  const names = use(MentionContext);
  const jumbo = use(JumboContext);
  const found = names.emoji?.(id) ?? null;
  if (found === null) return `:${name}:`;
  // Its name now: the text keeps the one it had when sent.
  const shown = `:${found.name ?? name}:`;
  return (
    <img
      src={found.url}
      alt={shown}
      title={shown}
      draggable={false}
      loading="lazy"
      data-emoji={id}
      className={`inline-block object-contain ${jumbo ? "my-0.5 size-12" : "-mt-1 size-[1.375em] align-middle"}`}
    />
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
    case "emoji":
      return <CustomEmoji key={key} name={node.name} id={node.id} />;
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
}) => <MarkdownBody source={source} after={after} mentions={mentions} />;

const MarkdownBody = ({ source, after, mentions }: { source: string; after: ReactNode; mentions: MentionNames }) => {
  const nodes = parseMarkdown(source);
  return (
    <div className="break-words whitespace-pre-wrap [overflow-wrap:anywhere]">
      <MentionContext value={mentions}>
        <JumboContext value={isJumbo(nodes)}>{renderMarkdown(nodes)}</JumboContext>
      </MentionContext>
      {after}
    </div>
  );
};

/** Stands in for a hidden spoiler in a one-line preview (as markdownText does). */
const LINE_SPOILER = "▒▒▒▒";

/** Nodes as one line: blocks flatten to inline, line breaks become spaces, links and spoilers stay inert. */
const renderLine = (nodes: MdNode[], names: MentionNames): ReactNode[] => nodes.map((n, i) => lineNode(n, i, names));

const lineNode = (n: MdNode, key: number, names: MentionNames): ReactNode => {
  switch (n.type) {
    case "text":
      return n.value.replace(/\s+/g, " ");
    case "bold":
      return <strong key={key}>{renderLine(n.children, names)}</strong>;
    case "italic":
      return <em key={key}>{renderLine(n.children, names)}</em>;
    case "underline":
      return <u key={key}>{renderLine(n.children, names)}</u>;
    case "strike":
      return <s key={key}>{renderLine(n.children, names)}</s>;
    case "spoiler":
      return LINE_SPOILER;
    case "code":
      return (
        <code key={key} className="rounded-xs bg-code px-1 font-mono text-[0.85em]">
          {n.value.replace(/\s+/g, " ")}
        </code>
      );
    // Blocks stand apart from the text around them.
    case "codeBlock":
      return (
        <span key={key}>
          {" "}
          <code className="rounded-xs bg-code px-1 font-mono text-[0.85em]">
            {n.value.trim().replace(/\s+/g, " ")}
          </code>{" "}
        </span>
      );
    case "quote":
      return <span key={key}> {renderLine(n.children, names)} </span>;
    case "link":
      return <span key={key}>{renderLine(n.children, names)}</span>;
    case "mention":
      return (
        <span key={key} className="text-fg-mention">
          {mentionText(names, n.kind, n.id)}
        </span>
      );
    case "emoji":
      return `:${n.name}:`;
  }
};

/**
 * A message as one line with its inline marks (bold, italic, code,
 * mentions), for the message a reply answers. Nothing in it is
 * interactive: the whole line is the way to the original.
 */
export const MarkdownLine = ({ source, mentions = NO_NAMES }: { source: string; mentions?: MentionNames }) => (
  <>{renderLine(parseMarkdown(source), mentions)}</>
);
