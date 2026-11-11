package messages

import (
	"slices"
	"testing"
)

func TestPreviewURLs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    []string
	}{
		{"code", "`https://skip.test`\n```\nhttps://skip.test\n``` https://keep.test", []string{"https://keep.test"}},
		{"suppressed", "<https://skip.test> http://keep.test", []string{"http://keep.test"}},
		{"punctuation", "(https://wiki.test/Foo_(bar)). https://example.test/what?!", []string{"https://wiki.test/Foo_(bar)", "https://example.test/what"}},
		{"limit", "https://a.test https://a.test https://b.test https://c.test https://d.test https://e.test https://f.test", []string{"https://a.test", "https://b.test", "https://c.test", "https://d.test", "https://e.test"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := previewURLs(tc.content); !slices.Equal(got, tc.want) {
				t.Errorf("previewURLs = %v, want %v", got, tc.want)
			}
		})
	}
}
