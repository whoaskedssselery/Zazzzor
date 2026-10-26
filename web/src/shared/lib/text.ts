/**
 * Length in Unicode code points ("runes"), the unit of every text limit on the
 * server (Go's utf8.RuneCountInString). "👍" is one rune but two UTF-16 units.
 */
export function runeLength(s: string): number {
  return Array.from(s).length;
}

// Exactly the characters Go's unicode.IsSpace reports: the Latin-1 spaces plus
// the Unicode White_Space property. Not JavaScript's \s: that one also matches
// U+FEFF and misses U+0085, so client and server would disagree on "empty".
// All of them are in the BMP, so one UTF-16 unit each.
// biome-ignore format: one row per Unicode block, as the list is read against Go's.
const GO_SPACES = new Set([
  0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x20, 0x85, 0xa0, 0x1680,
  0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a,
  0x2028, 0x2029, 0x202f, 0x205f, 0x3000,
]);

/** Trims message content the way the server does (strings.TrimFunc(s, unicode.IsSpace)). */
export function trimContent(s: string): string {
  let start = 0;
  let end = s.length;
  while (start < end && GO_SPACES.has(s.charCodeAt(start))) start++;
  while (end > start && GO_SPACES.has(s.charCodeAt(end - 1))) end--;
  return s.slice(start, end);
}
