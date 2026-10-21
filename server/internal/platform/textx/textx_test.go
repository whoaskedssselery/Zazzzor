package textx_test

import (
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/textx"
)

// cps builds a string from code points, keeping invisible characters out of
// the source.
func cps(r ...rune) string { return string(r) }

func TestHasVisible(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"spaces and tabs", " \t\n\r", false},
		{"no-break and ideographic spaces", cps(0x00A0, 0x3000, 0x2007), false},
		{"line and paragraph separators", cps(0x2028, 0x2029), false},
		{"zero width space and joiners", cps(0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF), false},
		{"soft hyphen", cps(0x00AD), false},
		{"lrm rlm", cps(0x200E, 0x200F), false},
		{"bidi controls", cps(0x202E, 0x2066), false},
		{"combining marks only", cps(0x0301, 0x0308, 0x20DD), false},
		{"variation selector", cps(0xFE0F), false},
		{"hangul fillers", cps(0x115F, 0x1160, 0x3164, 0xFFA0), false},
		{"braille blank", cps(0x2800), false},
		{"control chars", "\x00\x07\x7f", false},
		{"tag characters", cps(0xE0041, 0xE007F), false},
		{"latin", "a", true},
		{"cyrillic", "Вася", true},
		{"hebrew with rlm", "שלום" + cps(0x200F), true},
		{"emoji", cps(0x1F600), true},
		{"emoji zwj sequence", cps(0x1F469, 0x200D, 0x1F4BB), true},
		{"accent on a letter", "e" + cps(0x0301), true},
		{"filler then letter", cps(0x3164) + "x", true},
		{"punctuation", ".", true},
		{"cjk", "漢", true},
	}
	for _, tc := range cases {
		if got := textx.HasVisible(tc.in); got != tc.want {
			t.Errorf("%s: HasVisible(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestContainsBidiControl(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"plain", "hello", false},
		{"lrm and rlm are allowed", "a" + cps(0x200E) + "b" + cps(0x200F), false},
		{"arabic letter mark is allowed", "a" + cps(0x061C), false},
		{"hebrew", "שלום", false},
		{"LRE", "a" + cps(0x202A), true},
		{"RLE", cps(0x202B), true},
		{"PDF", cps(0x202C), true},
		{"LRO", cps(0x202D), true},
		{"RLO", "evil" + cps(0x202E) + "gpj.exe", true},
		{"LRI", cps(0x2066), true},
		{"RLI", cps(0x2067), true},
		{"FSI", cps(0x2068), true},
		{"PDI", cps(0x2069), true},
		{"neighbours are fine", cps(0x2029, 0x206A), false},
	}
	for _, tc := range cases {
		if got := textx.ContainsBidiControl(tc.in); got != tc.want {
			t.Errorf("%s: ContainsBidiControl(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}
