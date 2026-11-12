import { type MdNode, parseMarkdown } from "./parse";
import { type MentionNames, mentionText } from "./render";

/** Stands in for a spoiler's text, which a one-line preview must not give away. */
export const SPOILER_TEXT = "▒▒▒▒";

const NO_NAMES: MentionNames = { user: () => null, role: () => null };

const text = (nodes: MdNode[], names: MentionNames): string =>
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
          return ` ${text(n.children, names)} `;
        case "spoiler":
          return SPOILER_TEXT;
        case "mention":
          return mentionText(names, n.kind, n.id);
        case "emoji":
          return `:${n.name}:`;
        default:
          return text(n.children, names);
      }
    })
    .join("");

/**
 * A message as one line of plain text, for previews (the message a reply
 * answers): the markdown's text without its marks, spoilers hidden, line
 * breaks and runs of spaces as one space.
 */
export const markdownText = (src: string, names: MentionNames = NO_NAMES): string =>
  text(parseMarkdown(src), names).replace(/\s+/g, " ").trim();
