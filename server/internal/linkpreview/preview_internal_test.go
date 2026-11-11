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
	media := saveImage(context.Background(), payload, func(_ context.Context, _ []byte, mime string) (string, error) {
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

func TestFetchYouTubeVideo(t *testing.T) {
	f := New()
	var requested []string
	f.client.Transport = previewTransport(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}},
			Body: io.NopCloser(strings.NewReader("<title>Video title</title>")), Request: r}, nil
	})
	save := func(context.Context, []byte, string) (string, error) { return "", nil }
	if got := f.Fetch(t.Context(), "https://youtu.be/invalid", save); got != nil {
		t.Fatalf("invalid ID produced an embed: %+v", got)
	}
	if len(requested) != 0 {
		t.Fatalf("invalid ID issued requests: %v", requested)
	}
	got := f.Fetch(t.Context(), "https://music.youtube.com/watch?v=dQw4w9WgXcQ", save)
	if got == nil || got.Type != "video" || got.Video == nil ||
		got.Video.URL != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" ||
		got.Title == nil || *got.Title != "Video title" {
		t.Fatalf("video = %+v, want privacy-mode player and title", got)
	}
	if len(requested) != 2 || requested[0] != "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg" {
		t.Errorf("requests = %v, want fixed thumbnail then watch page", requested)
	}
}
