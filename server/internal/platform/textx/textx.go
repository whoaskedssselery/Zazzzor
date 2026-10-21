// Package textx holds checks on user-supplied text shared by the modules
// (display names, later messages).
package textx

import "unicode"

// fillers are code points that render as blank space although Unicode does
// not class them as spaces or marks: the Hangul fillers and the blank
// Braille pattern, popular for "invisible" names.
var fillers = map[rune]bool{
	0x115F: true, // HANGUL CHOSEONG FILLER
	0x1160: true, // HANGUL JUNGSEONG FILLER
	0x3164: true, // HANGUL FILLER
	0xFFA0: true, // HALFWIDTH HANGUL FILLER
	0x2800: true, // BRAILLE PATTERN BLANK
}

// invisible reports whether r draws nothing on its own: white space and
// other separators (Z*), control (Cc) and format (Cf: zero-width spaces and
// joiners, bidi marks, tags, ...) characters, combining marks (M*: accents,
// variation selectors) and the fillers above.
func invisible(r rune) bool {
	return unicode.IsSpace(r) || unicode.In(r, unicode.Z, unicode.Cc, unicode.Cf, unicode.M) || fillers[r]
}

// HasVisible reports whether s contains at least one character that draws
// something. A string made only of spaces, zero-width characters, combining
// marks or filler characters has none.
func HasVisible(s string) bool {
	for _, r := range s {
		if !invisible(r) {
			return true
		}
	}
	return false
}

// ContainsBidiControl reports whether s contains a bidirectional embedding,
// override or isolate (U+202A..U+202E, U+2066..U+2069). These reorder the
// text that follows them, which lets a name or message display as something
// it is not ("evil" + RLO + "gpj.exe" reads as a .jpg). The implicit marks
// LRM, RLM (U+200E, U+200F) and ALM (U+061C) are not included: they only
// nudge the direction of neutral characters next to them and are
// legitimately used in Hebrew and Arabic text.
func ContainsBidiControl(s string) bool {
	for _, r := range s {
		if (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			return true
		}
	}
	return false
}
