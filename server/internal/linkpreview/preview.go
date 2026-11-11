package linkpreview

import (
	"bytes"
	"context"
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

// Fetch builds an embed within one time budget, including any thumbnail.
func (f *Fetcher) Fetch(ctx context.Context, raw string, save SaveImage) *Embed {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u, err := url.Parse(raw)
	if err != nil || validateURL(u) != nil {
		return nil
	}
	if id, video := youtubeID(u); video {
		if id == "" {
			return nil
		}
		return f.video(ctx, raw, id, save)
	}
	res, err := f.read(ctx, raw)
	if err != nil {
		return nil
	}
	if id, video := youtubeID(res.url); video {
		if id == "" {
			return nil
		}
		return f.video(ctx, raw, id, save)
	}
	e := &Embed{URL: raw, Type: "link"}
	switch res.mediaType {
	case "text/html":
		metadata := parseMetadata(res.data, res.contentType)
		e.Title, e.Description, e.SiteName = optional(metadata.title), optional(metadata.description), optional(metadata.siteName)
		if metadata.image != "" {
			if imageURL, resolveErr := metadata.imageURL(res.url); resolveErr == nil {
				e.Image = f.thumbnail(ctx, imageURL.String(), save)
			}
		}
		if e.Title == nil && e.Description == nil && e.SiteName == nil && e.Image == nil {
			return nil
		}
	default:
		e.Type = "image"
		e.Image = saveImage(ctx, res, save)
		if e.Image == nil {
			return nil
		}
	}
	return e
}

// thumbnail applies the same request rules and image validation as direct links.
func (f *Fetcher) thumbnail(ctx context.Context, raw string, save SaveImage) *Media {
	res, err := f.read(ctx, raw)
	if err != nil {
		return nil
	}
	return saveImage(ctx, res, save)
}

// saveImage accepts only supported signatures with decodable dimensions.
func saveImage(ctx context.Context, res response, save SaveImage) *Media {
	mime := http.DetectContentType(res.data)
	if mime != res.mediaType || !strings.HasPrefix(mime, "image/") {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(res.data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil
	}
	src, err := save(ctx, res.data, mime)
	if err != nil {
		return nil
	}
	return &Media{URL: src, Width: cfg.Width, Height: cfg.Height}
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

// video uses the canonical watch page for its title and a fixed privacy-mode player.
func (f *Fetcher) video(ctx context.Context, raw, id string, save SaveImage) *Embed {
	e := &Embed{URL: raw, Type: "video", SiteName: optional("YouTube"),
		Video: &Media{URL: "https://www.youtube-nocookie.com/embed/" + id, Width: 560, Height: 315}}
	// Fetch the thumbnail first so a slow watch page cannot consume its budget.
	e.Image = f.thumbnail(ctx, "https://i.ytimg.com/vi/"+id+"/hqdefault.jpg", save)
	if res, err := f.read(ctx, "https://www.youtube.com/watch?v="+id); err == nil && res.mediaType == "text/html" {
		metadata := parseMetadata(res.data, res.contentType)
		e.Title, e.Description = optional(metadata.title), optional(metadata.description)
	}
	return e
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
