package messages

import (
	"slices"
	"testing"
)

func TestParseMentionsKeepsAppearanceOrder(t *testing.T) {
	content := "<@42>, <@&15> <@42> @here and @everyone"
	want := []mentionToken{
		{kind: mentionUser, id: 42},
		{kind: mentionRole, id: 15},
		{kind: mentionUser, id: 42},
		{kind: mentionHere},
		{kind: mentionEveryone},
	}
	if got := parseMentions(content); !slices.Equal(got, want) {
		t.Fatalf("parseMentions(%q) = %v, want %v", content, got, want)
	}
}

func TestParseMentionsRequiresCanonicalSnowflakes(t *testing.T) {
	content := "<@0> <@01> <@&+1> <@!-2> <@> <@&9223372036854775808> <@9223372036854775807>"
	want := []mentionToken{{kind: mentionUser, id: 9223372036854775807}}
	if got := parseMentions(content); !slices.Equal(got, want) {
		t.Fatalf("parseMentions(%q) = %v, want %v", content, got, want)
	}
}

func TestParseMentionsRequiresWordBoundariesForBroadcasts(t *testing.T) {
	letter := string(rune(0x0430))
	content := "abc@everyone @everyone2 _@here @here_ " + letter + "@here @everyone" + letter + " (@everyone), @here!"
	want := []mentionToken{{kind: mentionEveryone}, {kind: mentionHere}}
	if got := parseMentions(content); !slices.Equal(got, want) {
		t.Fatalf("parseMentions(%q) = %v, want %v", content, got, want)
	}
}

func TestParseMentionsSkipsCodeWithMatchingBacktickRuns(t *testing.T) {
	content := "`<@1>` ``<@&2> ` @here``\n```go\n<@3>\n```\n````txt\n``` @everyone <@&4>\n```` <@5>"
	want := []mentionToken{{kind: mentionUser, id: 5}}
	if got := parseMentions(content); !slices.Equal(got, want) {
		t.Fatalf("parseMentions(%q) = %v, want %v", content, got, want)
	}
}

func TestParseMentionsReadsTextAfterUnclosedBackticks(t *testing.T) {
	content := "``` @here `<@1>` <@2>"
	want := []mentionToken{{kind: mentionHere}, {kind: mentionUser, id: 2}}
	if got := parseMentions(content); !slices.Equal(got, want) {
		t.Fatalf("parseMentions(%q) = %v, want %v", content, got, want)
	}
}

func TestParseMentionsReadsTextAfterEscapedOpeningBacktick(t *testing.T) {
	content := "\\`<@1>` @here"
	want := []mentionToken{{kind: mentionUser, id: 1}, {kind: mentionHere}}
	if got := parseMentions(content); !slices.Equal(got, want) {
		t.Fatalf("parseMentions(%q) = %v, want %v", content, got, want)
	}
}
