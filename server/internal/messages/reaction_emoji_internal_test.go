package messages

import (
	"strings"
	"testing"
)

func TestValidReactionEmoji(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		emoji string
		want  bool
	}{
		{"empty", "", false},
		{"pictograph", "👍", true},
		{"modifier", "👍🏽", true},
		{"family", string([]rune{0x1F468, 0x200D, 0x1F469, 0x200D, 0x1F467}), true},
		{"regional pair", "🇷🇺", true},
		{"variation selector", "❤️", true},
		{"malformed UTF-8", string([]byte{0xff}), false},
		{"whitespace", "👍 ", false},
		{"control", "👍" + string(rune(0x0000)), false},
		{"text", "a", false},
		{"two clusters", "👍👍", false},
		{"three indicators", "🇷🇺🇵", false},
		{"single indicator", "🇷", false},
		{"keycap without pictograph", "1️⃣", false},
		{"32 bytes", "👍" + strings.Repeat(string(rune(0x0301)), 14), true},
		{"over 32 bytes", "👍" + strings.Repeat(string(rune(0x0301)), 15), false},
		{"ZWJ followed by non-pictograph", "👍" + string(rune(0x200D)) + "a", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validReactionEmoji(tc.emoji); got != tc.want {
				t.Errorf("validReactionEmoji(%q) = %v, want %v", tc.emoji, got, tc.want)
			}
		})
	}
}
