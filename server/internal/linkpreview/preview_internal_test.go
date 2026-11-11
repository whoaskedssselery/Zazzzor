package linkpreview

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMetadataFallbacks(t *testing.T) {
	for _, tc := range []struct{ html, title, description string }{
		{`<title>old</title><meta property="og:title" content="Open &amp; Graph"><meta name="twitter:title" content="tweet"><meta name="twitter:description" content="summary">`, "Open & Graph", "summary"},
		{`<meta name="twitter:title" content="Twitter"><title>old</title><meta name="description" content="fallback">`, "Twitter", "fallback"},
		{`<title>Plain</title>`, "Plain", ""},
		{"<title>" + string([]byte{0xcf, 0xf0, 0xe8, 0xe2, 0xe5, 0xf2}) + "</title>", "Привет", ""},
		{`<meta property="og:title" content="` + strings.Repeat("я", 260) + `"><meta property="og:description" content="` + strings.Repeat("я", 360) + `">`, strings.Repeat("я", 256), strings.Repeat("я", 350)},
	} {
		contentType := "text/html; charset=utf-8"
		if tc.title == "Привет" {
			contentType = "text/html; charset=windows-1251"
		}
		got := parseMetadata([]byte(tc.html), contentType)
		if got.title != tc.title || got.description != tc.description {
			t.Errorf("metadata = %q, %q, want %q, %q", got.title, got.description, tc.title, tc.description)
		}
	}
}

func TestYouTubeForms(t *testing.T) {
	for _, target := range []string{
		"https://youtube.com/watch?v=dQw4w9WgXcQ", "https://youtu.be/dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ", "https://m.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
	} {
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := youtubeID(u); got != "dQw4w9WgXcQ" {
			t.Errorf("youtubeID(%q) = %q, want dQw4w9WgXcQ", target, got)
		}
	}
	for _, target := range []string{"https://youtu.be/wrong", "https://youtube.com.example/watch?v=dQw4w9WgXcQ"} {
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := youtubeID(u); got != "" {
			t.Errorf("youtubeID(%q) = %q, want empty", target, got)
		}
	}
}

func TestImageEmbedDimensions(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 4))); err != nil {
		t.Fatal(err)
	}
	payload := response{data: buf.Bytes(), mediaType: "image/png"}
	media, _ := saveImage(context.Background(), payload, func(_ context.Context, _ []byte, mime string) (string, error) {
		if mime != "image/png" {
			t.Errorf("type = %s, want image/png", mime)
		}
		return "/api/v1/embeds/123", nil
	})
	if media == nil || media.Width != 3 || media.Height != 4 || media.URL != "/api/v1/embeds/123" {
		t.Errorf("image = %+v, want 3x4 proxied image", media)
	}
}

type previewTransport func(*http.Request) (*http.Response, error)

func (f previewTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// pngBytes is a w×h PNG.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func reply(r *http.Request, contentType string, body []byte) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}},
		Body: io.NopCloser(bytes.NewReader(body)), Request: r}
}

func TestFetchYouTubeVideo(t *testing.T) {
	f := New()
	var requested []string
	thumb := pngBytes(t, 4, 3)
	f.client.Transport = previewTransport(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		if r.URL.Host == "i.ytimg.com" {
			return reply(r, "image/png", thumb), nil
		}
		return reply(r, "application/json", []byte(`{"title":"Never Gonna Give You Up","author_name":"Rick Astley"}`)), nil
	})
	save := func(context.Context, []byte, string) (string, error) { return "/api/v1/embeds/T", nil }
	if got, _ := f.Fetch(t.Context(), "https://youtu.be/invalid", save); got != nil {
		t.Fatalf("invalid ID produced an embed: %+v", got)
	}
	if len(requested) != 0 {
		t.Fatalf("invalid ID issued requests: %v", requested)
	}
	got, outcome := f.Fetch(t.Context(), "https://music.youtube.com/watch?v=dQw4w9WgXcQ", save)
	if got == nil || got.Type != "video" || got.Video == nil ||
		got.Video.URL != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" ||
		got.Title == nil || *got.Title != "Never Gonna Give You Up" ||
		got.Description == nil || *got.Description != "Rick Astley" || got.Image == nil || outcome != (Outcome{}) {
		t.Fatalf("video = %+v, %+v, want player, oEmbed title and channel, cover", got, outcome)
	}
	wantOEmbed := "https://www.youtube.com/oembed?format=json&url=" + url.QueryEscape("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if len(requested) != 2 || requested[0] != "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg" || requested[1] != wantOEmbed {
		t.Errorf("requests = %v, want the cover then oEmbed", requested)
	}
}

func TestFetchYouTubeWithoutOEmbedKeepsThePlayer(t *testing.T) {
	f := New()
	f.client.Transport = previewTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "i.ytimg.com" {
			return reply(r, "image/png", pngBytes(t, 4, 3)), nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	got, outcome := f.Fetch(t.Context(), "https://youtu.be/dQw4w9WgXcQ", func(context.Context, []byte, string) (string, error) { return "/x", nil })
	if got == nil || got.Video == nil || got.Title != nil {
		t.Fatalf("video = %+v, want a player without a title", got)
	}
	if outcome != (Outcome{Partial: true, Stage: "oembed", Reason: "HTTP 404"}) {
		t.Errorf("outcome = %+v, want a partial oEmbed failure", outcome)
	}
}

func TestPictureHasItsOwnBudget(t *testing.T) {
	f := New()
	f.budget = 300 * time.Millisecond
	f.client.Timeout = time.Second
	f.client.Transport = previewTransport(func(r *http.Request) (*http.Response, error) {
		// Each answer takes most of a budget: together they exceed one.
		select {
		case <-time.After(200 * time.Millisecond):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		if r.URL.Path == "/og.png" {
			return reply(r, "image/png", pngBytes(t, 600, 300)), nil
		}
		return reply(r, "text/html", []byte(`<meta property="og:title" content="Page"><meta property="og:image" content="/og.png">`)), nil
	})
	got, outcome := f.Fetch(t.Context(), "https://slow.example/page", func(context.Context, []byte, string) (string, error) { return "/x", nil })
	if got == nil || got.Image == nil || outcome.Partial {
		t.Fatalf("embed = %+v, %+v, want the picture despite a slow page", got, outcome)
	}
}

func TestLostPictureIsPartial(t *testing.T) {
	f := New()
	f.client.Transport = previewTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/og.png" {
			return reply(r, "text/plain", []byte("nope")), nil
		}
		return reply(r, "text/html", []byte(`<meta property="og:title" content="Page"><meta property="og:image" content="/og.png">`)), nil
	})
	got, outcome := f.Fetch(t.Context(), "https://example.org/page", func(context.Context, []byte, string) (string, error) { return "/x", nil })
	if got == nil || got.Image != nil || outcome != (Outcome{Partial: true, Stage: "image", Reason: "content type"}) {
		t.Fatalf("embed = %+v, %+v, want a card marked partial for its picture", got, outcome)
	}
}

func TestJSONIsNoLink(t *testing.T) {
	f := New()
	f.client.Transport = previewTransport(func(r *http.Request) (*http.Response, error) {
		return reply(r, "application/json", []byte(`{"title":"x"}`)), nil
	})
	got, outcome := f.Fetch(t.Context(), "https://api.example.org/thing", func(context.Context, []byte, string) (string, error) { return "/x", nil })
	if got != nil || outcome != (Outcome{Stage: "page", Reason: "content type"}) {
		t.Fatalf("embed = %+v, %+v, want no preview of a JSON link", got, outcome)
	}
}

func TestHostForLogs(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:secret@Example.ORG/reset?token=abc#x": "example.org",
		"http://[::ffff:8.8.8.8]/a":                         "8.8.8.8",
		"::not a url":                                       "invalid",
	} {
		if got := Host(raw); got != want {
			t.Errorf("Host(%q) = %q, want %q", raw, got, want)
		}
	}
}
