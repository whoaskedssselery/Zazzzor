package messages

import (
	"sort"
	"unicode"
	"unicode/utf8"
)

const (
	maxReactionEmojiBytes = 32
	zeroWidthJoiner       = 0x200D
)

type emojiRuneRange struct{ lo, hi rune }

// inEmojiRanges looks up a rune in a sorted table of disjoint ranges.
func inEmojiRanges(r rune, table []emojiRuneRange) bool {
	i := sort.Search(len(table), func(i int) bool { return table[i].hi >= r })
	return i < len(table) && table[i].lo <= r
}

// validReactionEmoji recognizes the subset of extended grapheme clusters
// containing a pictograph or a pair of regional indicators. With these
// required bases, only Prepend, Extend, SpacingMark, and ZWJ can attach
// without starting a new cluster (Unicode UAX #29, GB9 through GB13).
func validReactionEmoji(name string) bool {
	if name == "" || len(name) > maxReactionEmojiBytes || !utf8.ValidString(name) {
		return false
	}
	runes := []rune(name)
	for _, r := range runes {
		if unicode.IsSpace(r) || unicode.IsControl(r) || inEmojiRanges(r, graphemeControlRanges) {
			return false
		}
	}
	for len(runes) > 0 && inEmojiRanges(runes[0], graphemePrependRanges) {
		runes = runes[1:]
	}
	if len(runes) == 0 {
		return false
	}

	afterPictograph := inEmojiRanges(runes[0], pictographicRanges)
	if afterPictograph {
		runes = runes[1:]
	} else if len(runes) >= 2 && regionalIndicator(runes[0]) && regionalIndicator(runes[1]) {
		runes = runes[2:]
	} else {
		return false
	}

	joinNext := false
	for _, r := range runes {
		switch {
		case inEmojiRanges(r, graphemeExtendRanges):
			joinNext = false
		case inEmojiRanges(r, graphemeSpacingRanges):
			afterPictograph, joinNext = false, false
		case r == zeroWidthJoiner:
			afterPictograph, joinNext = false, afterPictograph
		case joinNext && inEmojiRanges(r, pictographicRanges):
			afterPictograph, joinNext = true, false
		default:
			return false
		}
	}
	return true
}

// regionalIndicator reports whether r is one of the 26 flag letters.
func regionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }
