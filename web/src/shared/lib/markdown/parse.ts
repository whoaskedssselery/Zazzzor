// Message markdown: the safe subset of Discord's. The parser turns text into
// a tree that the renderer turns into React elements, so nothing in a message
// is ever HTML. Every rule is a plain scan with indexOf, never a backtracking
// regular expression, and nesting stops at MAX_DEPTH: the 4000 characters of
// a message parse in linear-ish time, whatever they hold.

/** Inline styles; each wraps the nodes inside it. */
export type MdStyle = "bold" | "italic" | "underline" | "strike" | "spoiler";

export type MdNode =
  | { type: "text"; value: string }
  | { type: MdStyle; children: MdNode[] }
  | { type: "code"; value: string }
  | { type: "codeBlock"; lang: string | null; value: string }
  | { type: "quote"; children: MdNode[] }
  | { type: "link"; href: string; children: MdNode[] }
  | { type: "mention"; kind: MentionKind; id: string | null }
  | { type: "emoji"; name: string; id: string; animated: boolean };

/** What a mention points at: `<@id>` a user, `<@&id>` a role, `@everyone`, `@here`. */
export type MentionKind = "user" | "role" | "everyone" | "here";

/** Styles nest at most this deep; anything deeper stays text. */
export const MAX_DEPTH = 8;

/** Inline delimiters, longest first so `**` wins over `*`. */
const DELIMITERS: ReadonlyArray<readonly [string, MdStyle]> = [
  ["||", "spoiler"],
  ["**", "bold"],
  ["__", "underline"],
  ["~~", "strike"],
  ["*", "italic"],
  ["_", "italic"],
];

/** Characters a backslash makes literal. */
const ESCAPABLE = new Set(["\\", "*", "_", "~", "|", "`", "[", "]", "(", ")", ">", ":"]);

const WORD = /[\p{L}\p{N}]/u;
const isWord = (c: string | undefined): boolean => c !== undefined && WORD.test(c);
const isSpace = (c: string | undefined): boolean => c !== undefined && /\s/.test(c);

/** Characters that end a sentence rather than an address. */
const TRAILING = new Set([".", ",", ":", ";", "!", "?", "'", '"']);
const AUTOLINK = /https?:\/\/[^\s<]+/iy;
const LANG = /^([A-Za-z0-9_+.#-]{1,32})\n/;

/**
 * An http(s) address as a link target, or null. Only absolute http and https
 * addresses link: never javascript:, data:, vbscript: or a relative path.
 */
export function safeHref(raw: string): string | null {
  if (!/^https?:\/\//i.test(raw) || /\s/.test(raw)) return null;
  try {
    const url = new URL(raw);
    return (url.protocol === "http:" || url.protocol === "https:") && url.hostname !== "" ? url.href : null;
  } catch {
    return null;
  }
}

/** Joins neighbouring text nodes. */
const pushText = (out: MdNode[], value: string): void => {
  if (value === "") return;
  const last = out.at(-1);
  if (last?.type === "text") last.value += value;
  else out.push({ type: "text", value });
};

interface Match {
  node: MdNode;
  end: number;
}

/** Inline code at `i`: one or two backticks, closed by the same run. */
const inlineCode = (s: string, i: number): Match | null => {
  const fence = s.startsWith("``", i) ? "``" : "`";
  const close = s.indexOf(fence, i + fence.length);
  if (close <= i + fence.length) return null;
  return { node: { type: "code", value: s.slice(i + fence.length, close) }, end: close + fence.length };
};

const DIGIT = /[0-9]/;
/** Snowflake IDs: 1 to 20 digits. */
const MAX_ID = 20;

/** A user or role mention `<@123>` / `<@&123>` at `i`, as the server parses it (C8). */
const idMention = (s: string, i: number): Match | null => {
  if (!s.startsWith("<@", i)) return null;
  const role = s[i + 2] === "&";
  const from = i + (role ? 3 : 2);
  let k = from;
  while (k < s.length && k - from < MAX_ID && DIGIT.test(s.charAt(k))) k++;
  if (k === from || s[k] !== ">") return null;
  return { node: { type: "mention", kind: role ? "role" : "user", id: s.slice(from, k) }, end: k + 1 };
};

const EMOJI_NAME = /[A-Za-z0-9_]/;
/** Custom emoji names: 2 to 32 characters (C11). */
const MAX_EMOJI_NAME = 32;

/** A custom emoji `<:name:id>` or `<a:name:id>` (animated) at `i`, Discord's syntax. */
const customEmoji = (s: string, i: number): Match | null => {
  const animated = s.startsWith("<a:", i);
  if (!animated && !s.startsWith("<:", i)) return null;
  const from = i + (animated ? 3 : 2);
  let k = from;
  while (k < s.length && k - from <= MAX_EMOJI_NAME && EMOJI_NAME.test(s.charAt(k))) k++;
  if (k - from < 2 || k - from > MAX_EMOJI_NAME || s[k] !== ":") return null;
  const name = s.slice(from, k);
  const idFrom = k + 1;
  k = idFrom;
  while (k < s.length && k - idFrom < MAX_ID && DIGIT.test(s.charAt(k))) k++;
  if (k === idFrom || s[k] !== ">") return null;
  return { node: { type: "emoji", name, id: s.slice(idFrom, k), animated }, end: k + 1 };
};

/** `@everyone` or `@here` at `i`, as whole words. */
const groupMention = (s: string, i: number): Match | null => {
  if (isWord(s[i - 1])) return null;
  for (const kind of ["everyone", "here"] as const) {
    const end = i + 1 + kind.length;
    if (s.startsWith(kind, i + 1) && !isWord(s[end])) return { node: { type: "mention", kind, id: null }, end };
  }
  return null;
};

/** The end of a parenthesised link target starting at `from`, balancing nested parentheses. */
const targetEnd = (s: string, from: number): number => {
  let depth = 0;
  for (let k = from; k < s.length; k++) {
    const c = s[k];
    if (c === "\n") return -1;
    if (c === "(") depth++;
    else if (c === ")") {
      if (depth === 0) return k;
      depth--;
    }
  }
  return -1;
};

/** A masked link `[text](https://…)` at `i`; its text is styled but holds no links. */
const maskedLink = (s: string, i: number, depth: number): Match | null => {
  const mid = s.indexOf("](", i + 1);
  if (mid <= i + 1) return null;
  const end = targetEnd(s, mid + 2);
  if (end === -1) return null;
  const href = safeHref(s.slice(mid + 2, end));
  if (href === null) return null;
  const children = parseInline(s.slice(i + 1, mid), depth + 1, false);
  return { node: { type: "link", href, children }, end: end + 1 };
};

/** A bare http(s) address at `i`, without the punctuation that ends the sentence after it. */
const autolink = (s: string, i: number): Match | null => {
  if (i > 0 && (isWord(s[i - 1]) || s[i - 1] === "/")) return null;
  AUTOLINK.lastIndex = i;
  const m = AUTOLINK.exec(s);
  if (m === null) return null;
  let url = m[0];
  for (;;) {
    const last = url.at(-1);
    if (last !== undefined && TRAILING.has(last)) url = url.slice(0, -1);
    else if (last === ")" && url.split(")").length > url.split("(").length) url = url.slice(0, -1);
    else break;
  }
  const href = safeHref(url);
  if (href === null) return null;
  return { node: { type: "link", href, children: [{ type: "text", value: url }] }, end: i + url.length };
};

/** A styled span at `i`: an opening delimiter, non-empty content and the same delimiter closing it. */
const styled = (s: string, i: number, depth: number, links: boolean): Match | null => {
  for (const [token, type] of DELIMITERS) {
    if (!s.startsWith(token, i)) continue;
    const single = token.length === 1;
    // `_` styles whole words only, so snake_case stays as it is.
    if (token === "_" && isWord(s[i - 1])) continue;
    const from = i + token.length;
    if (single && isSpace(s[from])) continue;
    let close = s.indexOf(token, from);
    while (close !== -1) {
      const empty = close === from;
      const wordAfter = token === "_" && isWord(s[close + 1]);
      // A single `*` is not closed by the first half of a `**`.
      const doubled = single && s[close + 1] === token;
      if (!empty && !wordAfter && !doubled) break;
      close = s.indexOf(token, close + (doubled ? 2 : 1));
    }
    if (close === -1) continue;
    // In a longer run (`***`) a double delimiter closes at its end, leaving
    // the inner `*` to the italic inside: `**a *b***`.
    if (!single) while (s[close + token.length] === token[0]) close++;
    const children = parseInline(s.slice(from, close), depth + 1, links);
    return { node: { type, children }, end: close + token.length };
  }
  return null;
};

/** Inline markdown of `s`; `links` is false inside a masked link's text. */
function parseInline(s: string, depth: number, links: boolean): MdNode[] {
  if (depth > MAX_DEPTH) return s === "" ? [] : [{ type: "text", value: s }];
  const out: MdNode[] = [];
  let text = "";
  let i = 0;
  while (i < s.length) {
    const c = s.charAt(i);
    const next = s.charAt(i + 1);
    if (c === "\\" && ESCAPABLE.has(next)) {
      text += next;
      i += 2;
      continue;
    }
    let match: Match | null = null;
    if (c === "`") match = inlineCode(s, i);
    else if (c === "<") match = idMention(s, i) ?? customEmoji(s, i);
    else if (c === "@") match = groupMention(s, i);
    else if (c === "[" && links) match = maskedLink(s, i, depth);
    else if ((c === "h" || c === "H") && links) match = autolink(s, i);
    else if (c === "*" || c === "_" || c === "~" || c === "|") match = styled(s, i, depth, links);
    if (match === null) {
      text += c;
      i += 1;
      continue;
    }
    pushText(out, text);
    text = "";
    out.push(match.node);
    i = match.end;
  }
  pushText(out, text);
  return out;
}

/** Text between code blocks: quotes (`> ` lines, or `>>> ` to the end) and inline markdown. */
const parseBlocks = (src: string, out: MdNode[]): void => {
  if (src === "") return;
  const lines = src.split("\n");
  let plain: string[] = [];
  let quote: string[] = [];
  const flushPlain = () => {
    if (plain.length > 0) for (const n of parseInline(plain.join("\n"), 0, true)) push(out, n);
    plain = [];
  };
  const flushQuote = () => {
    if (quote.length > 0) out.push({ type: "quote", children: parseInline(quote.join("\n"), 1, true) });
    quote = [];
  };
  for (const [k, line] of lines.entries()) {
    if (line.startsWith(">>> ")) {
      flushPlain();
      flushQuote();
      quote = [line.slice(4), ...lines.slice(k + 1)];
      flushQuote();
      return;
    }
    if (line.startsWith("> ")) {
      flushPlain();
      quote.push(line.slice(2));
    } else {
      flushQuote();
      plain.push(line);
    }
  }
  flushPlain();
  flushQuote();
};

/** Appends a node, joining it with a text node before it. */
const push = (out: MdNode[], node: MdNode): void => {
  if (node.type === "text") pushText(out, node.value);
  else out.push(node);
};

/** The body of a fenced block: an optional language line, then the code (one edge newline dropped). */
const codeBlock = (body: string): MdNode | null => {
  if (body === "") return null;
  const lang = LANG.exec(body);
  let value = lang ? body.slice(lang[0].length) : body.replace(/^\n/, "");
  value = value.replace(/\n$/, "");
  return { type: "codeBlock", lang: lang?.[1] ?? null, value };
};

/**
 * Parses message markdown:
 * - `**bold**`, `*italic*` and `_italic_`, `__underline__`, `~~strike~~`, `||spoiler||`;
 * - `` `code` `` and fenced blocks with an optional language (no highlighting);
 * - `> quote` lines and `>>> ` quoting the rest;
 * - bare http(s) addresses and masked links `[text](https://…)`;
 * - mentions: `<@user_id>`, `<@&role_id>`, `@everyone`, `@here` (never in code);
 * - custom emoji: `<:name:id>` and `<a:name:id>`;
 * - backslash escapes. Line breaks stay in the text.
 * Anything else, HTML included, is text.
 */
export function parseMarkdown(src: string): MdNode[] {
  const out: MdNode[] = [];
  let pos = 0;
  let from = 0;
  for (;;) {
    const open = src.indexOf("```", from);
    if (open === -1) break;
    const close = src.indexOf("```", open + 3);
    if (close === -1) break;
    const block = codeBlock(src.slice(open + 3, close));
    if (block === null) {
      from = open + 3;
      continue;
    }
    // A block stands on its own lines: the line breaks around it are not text.
    parseBlocks(src.slice(pos, open).replace(/\n$/, ""), out);
    out.push(block);
    pos = close + 3;
    if (src[pos] === "\n") pos += 1;
    from = pos;
  }
  parseBlocks(src.slice(pos), out);
  return out;
}
