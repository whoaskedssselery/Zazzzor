import { type ReactNode, useState } from "react";
import { type MdNode, parseMarkdown } from "./parse";

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
 * A message's text with its markdown. Line breaks are kept and any string
 * wraps, however long, so nothing widens the chat.
 */
export const Markdown = ({ source }: { source: string }) => (
  <div className="break-words whitespace-pre-wrap [overflow-wrap:anywhere]">
    {renderMarkdown(parseMarkdown(source))}
  </div>
);
