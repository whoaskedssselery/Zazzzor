package linkpreview

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	_ "image/gif" // Register image formats supported by preview storage.
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	_ "golang.org/x/image/webp" // Register WebP with image.DecodeConfig.
)

// Embed is the metadata and optional proxied media for one original URL.
type Embed struct {
	URL         string  `json:"url"`
	Type        string  `json:"type"`
	SiteName    *string `json:"site_name"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Image       *Media  `json:"image"`
	Video       *Media  `json:"video"`
}

// Media is the URL and dimensions of an image or a video player.
type Media struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// SaveImage stores validated bytes and returns their same-origin URL.
type SaveImage func(context.Context, []byte, string) (string, error)

// Outcome tells how a preview went, for the cache and the log.
type Outcome struct {
	// Partial is set when the card was made but lost a part (its picture, a
	// video's title) that a later try may get: it is cached briefly.
	Partial bool
	// Stage ("page", "image", "oembed") and Reason of what failed; empty when nothing did.
	Stage, Reason string
}

func failed(stage, why string) Outcome { return Outcome{Stage: stage, Reason: why} }

// Fetch builds an embed for raw. The page, its picture and a video's
// oEmbed each get their own time budget, so a slow picture after a slow
// page is not lost. A nil embed means no preview; the outcome says why.
func (f *Fetcher) Fetch(ctx context.Context, raw string, save SaveImage) (*Embed, Outcome) {
	u, err := url.Parse(raw)
	if err != nil || validateURL(u) != nil {
		return nil, failed("page", "refused address")
	}
	if id, video := youtubeID(u); video {
		if id == "" {
			return nil, failed("page", "invalid video id")
		}
		return f.video(ctx, raw, id, save)
	}
	pageCtx, cancel := context.WithTimeout(ctx, f.budget)
	res, err := f.read(pageCtx, raw)
	cancel()
	if err != nil {
		return nil, failed("page", reason(err))
	}
	if id, video := youtubeID(res.url); video {
		if id == "" {
			return nil, failed("page", "invalid video id")
		}
		return f.video(ctx, raw, id, save)
	}
	e := &Embed{URL: raw, Type: "link"}
	var outcome Outcome
	switch res.mediaType {
	case "text/html":
		metadata := parseMetadata(res.data, res.contentType)
		e.Title, e.Description, e.SiteName = optional(metadata.title), optional(metadata.description), optional(metadata.siteName)
		if metadata.image != "" {
			if imageURL, resolveErr := metadata.imageURL(res.url); resolveErr == nil {
				var why string
				if e.Image, why = f.thumbnail(ctx, imageURL.String(), save); e.Image == nil {
					outcome = Outcome{Partial: true, Stage: "image", Reason: why}
				}
			}
		}
		if e.Title == nil && e.Description == nil && e.SiteName == nil && e.Image == nil {
			return nil, failed("page", "no metadata")
		}
	default:
		e.Type = "image"
		var why string
		if e.Image, why = saveImage(ctx, res, save); e.Image == nil {
			return nil, failed("image", why)
		}
	}
	return e, outcome
}

// thumbnail fetches a picture within its own budget, with the same request
// rules and image validation as direct links. On failure it says why.
func (f *Fetcher) thumbnail(ctx context.Context, raw string, save SaveImage) (*Media, string) {
	ctx, cancel := context.WithTimeout(ctx, f.budget)
	defer cancel()
	res, err := f.read(ctx, raw)
	if err != nil {
		return nil, reason(err)
	}
	return saveImage(ctx, res, save)
}

// saveImage accepts only supported signatures with decodable dimensions.
func saveImage(ctx context.Context, res response, save SaveImage) (*Media, string) {
	mime := http.DetectContentType(res.data)
	if mime != res.mediaType || !strings.HasPrefix(mime, "image/") {
		return nil, "content type"
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(res.data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, "undecodable image"
	}
	src, err := save(ctx, res.data, mime)
	if err != nil {
		return nil, "store"
	}
	return &Media{URL: src, Width: cfg.Width, Height: cfg.Height}, ""
}

var youtubePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// youtubeID accepts only the watch, shorts and short-link URL forms.
func youtubeID(u *url.URL) (string, bool) {
	host := canonicalHost(u.Hostname())
	var id string
	switch host {
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else if strings.HasPrefix(u.Path, "/shorts/") {
			id = strings.TrimPrefix(u.Path, "/shorts/")
		} else {
			return "", false
		}
	default:
		return "", false
	}
	if !youtubePattern.MatchString(id) {
		return "", true
	}
	return id, true
}

// oembed is the part of YouTube's oEmbed answer a card shows.
type oembed struct {
	Title      string `json:"title"`
	AuthorName string `json:"author_name"`
}

// video is a privacy-mode player with YouTube's cover, and the title and
// channel from oEmbed (the watch page is too long to read its tags).
func (f *Fetcher) video(ctx context.Context, raw, id string, save SaveImage) (*Embed, Outcome) {
	e := &Embed{URL: raw, Type: "video", SiteName: optional("YouTube"),
		Video: &Media{URL: "https://www.youtube-nocookie.com/embed/" + id, Width: 560, Height: 315}}
	var outcome Outcome
	var why string
	if e.Image, why = f.thumbnail(ctx, "https://i.ytimg.com/vi/"+id+"/hqdefault.jpg", save); e.Image == nil {
		outcome = Outcome{Partial: true, Stage: "image", Reason: why}
	}
	octx, cancel := context.WithTimeout(ctx, f.budget)
	defer cancel()
	watch := "https://www.youtube.com/watch?v=" + id
	res, err := f.readKind(octx, "https://www.youtube.com/oembed?format=json&url="+url.QueryEscape(watch), true)
	var o oembed
	switch {
	case err != nil:
		return e, Outcome{Partial: true, Stage: "oembed", Reason: reason(err)}
	case res.mediaType != "application/json" || json.Unmarshal(res.data, &o) != nil:
		return e, Outcome{Partial: true, Stage: "oembed", Reason: "content type"}
	}
	e.Title = optional(truncate(o.Title, maxTitle))
	e.Description = optional(truncate(o.AuthorName, maxDescription))
	return e, outcome
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
