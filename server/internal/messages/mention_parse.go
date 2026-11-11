package messages

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type mentionKind int

const (
	mentionUser mentionKind = iota
	mentionRole
	mentionEveryone
	mentionHere
)

type mentionToken struct {
	kind mentionKind
	id   snowflake.ID
}

// parseMentions finds syntax outside code in appearance order. The caller
// resolves membership, permissions and duplicate IDs before applying limits.
func parseMentions(content string) []mentionToken {
	var mentions []mentionToken
	for i := 0; i < len(content); {
		switch content[i] {
		case '\\':
			if i+1 < len(content) && (content[i+1] == '\\' || content[i+1] == '`') {
				i += 2
				continue
			}
		case '`':
			i = mentionCodeEnd(content, i)
			continue
		case '<':
			if token, width := parseIDMention(content[i:]); width > 0 {
				mentions = append(mentions, token)
				i += width
				continue
			}
		case '@':
			if token, width := parseBroadcastMention(content, i); width > 0 {
				mentions = append(mentions, token)
				i += width
				continue
			}
		}
		i++
	}
	return mentions
}

// mentionCodeEnd skips code closed by a run of the same length. A lone
// opener is literal text, so scanning continues immediately after its run.
func mentionCodeEnd(content string, start int) int {
	openEnd := backtickRunEnd(content, start)
	width := openEnd - start
	for from := openEnd; from < len(content); {
		next := strings.IndexByte(content[from:], '`')
		if next == -1 {
			break
		}
		closeStart := from + next
		closeEnd := backtickRunEnd(content, closeStart)
		if closeEnd-closeStart == width {
			return closeEnd
		}
		from = closeEnd
	}
	return openEnd
}

func backtickRunEnd(content string, i int) int {
	for i < len(content) && content[i] == '`' {
		i++
	}
	return i
}

// parseIDMention reads a user or role token starting with <@ and returns
// its byte width, or zero when the syntax or snowflake is invalid.
func parseIDMention(content string) (mentionToken, int) {
	if !strings.HasPrefix(content, "<@") {
		return mentionToken{}, 0
	}
	kind := mentionUser
	start := len("<@")
	if start < len(content) && content[start] == '&' {
		kind = mentionRole
		start++
	}
	end := start
	for end < len(content) && content[end] >= '0' && content[end] <= '9' {
		end++
	}
	if end == len(content) || content[end] != '>' {
		return mentionToken{}, 0
	}
	raw := content[start:end]
	id, err := snowflake.Parse(raw)
	if err != nil || id <= 0 || id.String() != raw {
		return mentionToken{}, 0
	}
	return mentionToken{kind: kind, id: id}, end + 1
}

// parseBroadcastMention recognizes the two bare words at rune boundaries.
func parseBroadcastMention(content string, i int) (mentionToken, int) {
	before, _ := utf8.DecodeLastRuneInString(content[:i])
	if isMentionWordRune(before) {
		return mentionToken{}, 0
	}
	var word string
	var kind mentionKind
	switch {
	case strings.HasPrefix(content[i:], "@everyone"):
		word, kind = "@everyone", mentionEveryone
	case strings.HasPrefix(content[i:], "@here"):
		word, kind = "@here", mentionHere
	default:
		return mentionToken{}, 0
	}
	after, _ := utf8.DecodeRuneInString(content[i+len(word):])
	if isMentionWordRune(after) {
		return mentionToken{}, 0
	}
	return mentionToken{kind: kind}, len(word)
}

func isMentionWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
}
