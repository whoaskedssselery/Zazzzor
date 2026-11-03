import { type MdNode, parseMarkdown } from "./parse";

/** Stands in for a spoiler's text, which a one-line preview must not give away. */
export const SPOILER_TEXT = "▒▒▒▒";

const text = (nodes: MdNode[]): string =>
  nodes
    .map((n) => {
      switch (n.type) {
        case "text":
        case "code":
          return n.value;
        // Blocks stand apart from the text around them.
        case "codeBlock":
          return ` ${n.value} `;
        case "quote":
          return ` ${text(n.children)} `;
        case "spoiler":
          return SPOILER_TEXT;
        default:
          return text(n.children);
      }
    })
    .join("");

/**
 * A message as one line of plain text, for previews (the message a reply
 * answers): the markdown's text without its marks, spoilers hidden, line
 * breaks and runs of spaces as one space.
 */
export const markdownText = (src: string): string => text(parseMarkdown(src)).replace(/\s+/g, " ").trim();
