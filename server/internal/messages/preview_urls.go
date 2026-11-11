package messages

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/whoaskedssselery/zazzzor/server/internal/linkpreview"
)

const maxEmbeds = 5

// previewURLs reads distinct links in appearance order, using the mention
// parser's handling of backticks and escapes.
func previewURLs(content string) []string {
	var urls []string
	seen := make(map[string]bool)
	for i := 0; i < len(content) && len(urls) < maxEmbeds; {
		if content[i] == '\\' && i+1 < len(content) && (content[i+1] == '\\' || content[i+1] == '`') {
			i += 2
			continue
		}
		if content[i] == '`' {
			i = mentionCodeEnd(content, i)
			continue
		}
		rest := content[i:]
		if !strings.HasPrefix(strings.ToLower(rest[:min(len(rest), 8)]), "http://") &&
			!strings.HasPrefix(strings.ToLower(rest[:min(len(rest), 8)]), "https://") {
			i++
			continue
		}
		end := strings.IndexFunc(rest, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("<>\"`", r)
		})
		if end < 0 {
			end = len(rest)
		}
		candidate := trimPreviewURL(rest[:end])
		suppressed := i > 0 && content[i-1] == '<' && end < len(rest) && rest[end] == '>'
		before, _ := utf8.DecodeLastRuneInString(content[:i])
		if !suppressed && !isMentionWordRune(before) && !seen[candidate] && len(candidate) <= linkpreview.MaxURL {
			if u, err := url.Parse(candidate); err == nil && u.Hostname() != "" {
				urls = append(urls, candidate)
				seen[candidate] = true
			}
		}
		i += end
	}
	return urls
}

// trimPreviewURL keeps closing parentheses that balance a URL's own opening ones.
func trimPreviewURL(s string) string {
	balance := strings.Count(s, "(") - strings.Count(s, ")")
	for len(s) > 0 {
		switch s[len(s)-1] {
		case '.', ',', '!', '?', ':', ';':
		case ')':
			if balance >= 0 {
				return s
			}
			balance++
		default:
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}
