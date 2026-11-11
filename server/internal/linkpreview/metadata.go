package linkpreview

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

type metadata struct {
	title, description, siteName, image, canonicalURL string
}

// parseMetadata reads the head with per-field OpenGraph, Twitter and HTML fallbacks.
func parseMetadata(raw []byte, contentType string) metadata {
	var reader io.Reader = bytes.NewReader(raw)
	encoding, name, certain := charset.DetermineEncoding(raw, contentType)
	if certain || name != "windows-1252" || !utf8.Valid(raw) {
		reader = encoding.NewDecoder().Reader(reader)
	}
	tags := map[string]string{}
	var title string
	z := html.NewTokenizer(reader)
scan:
	for {
		switch z.Next() {
		case html.ErrorToken:
			break scan
		case html.EndTagToken:
			if tag, _ := z.TagName(); string(tag) == "head" {
				break scan
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			switch token.Data {
			case "body":
				break scan
			case "title":
				if z.Next() == html.TextToken && title == "" {
					title = strings.TrimSpace(string(z.Text()))
				}
			case "meta":
				var property, name, value string
				for _, a := range token.Attr {
					switch a.Key {
					case "property":
						property = a.Val
					case "name":
						name = a.Val
					case "content":
						value = strings.TrimSpace(a.Val)
					}
				}
				for _, key := range []string{property, name} {
					key = strings.ToLower(key)
					if key != "" && tags[key] == "" {
						tags[key] = value
					}
				}
			}
		}
	}
	return metadata{
		canonicalURL: tags["og:url"],
		title:        truncate(first(tags["og:title"], tags["twitter:title"], title), 256),
		description:  truncate(first(tags["og:description"], tags["twitter:description"], tags["description"]), 350),
		siteName:     truncate(first(tags["og:site_name"], tags["twitter:site"]), 256),
		image:        first(tags["og:image"], tags["twitter:image"], tags["twitter:image:src"]),
	}
}

// imageURL resolves relative OG media against a valid same-origin canonical
// URL when supplied. Embed identity remains the link found in the message.
func (m metadata) imageURL(page *url.URL) (*url.URL, error) {
	if m.canonicalURL != "" {
		canonical, err := page.Parse(m.canonicalURL)
		if err == nil && validateURL(canonical) == nil && canonical.Scheme == page.Scheme &&
			canonicalHost(canonical.Hostname()) == canonicalHost(page.Hostname()) &&
			canonical.Port() == page.Port() {
			page = canonical
		}
	}
	u, err := page.Parse(m.image)
	if err != nil {
		return nil, fmt.Errorf("linkpreview: resolve image URL: %w", err)
	}
	return u, nil
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// truncate preserves rune boundaries and strips outer whitespace.
func truncate(s string, n int) string {
	for i := range s {
		if n == 0 {
			return strings.TrimSpace(s[:i])
		}
		n--
	}
	return s
}
