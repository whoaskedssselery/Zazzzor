package messages_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/linkpreview"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

type previewFetchFunc func(context.Context, string, linkpreview.SaveImage) *linkpreview.Embed

func (f previewFetchFunc) Fetch(ctx context.Context, url string, save linkpreview.SaveImage) (*linkpreview.Embed, linkpreview.Outcome) {
	return f(ctx, url, save), linkpreview.Outcome{}
}

// partialFetch makes cards that lost their picture, counting the fetches.
type partialFetch struct{ calls *atomic.Int32 }

func (f partialFetch) Fetch(_ context.Context, url string, _ linkpreview.SaveImage) (*linkpreview.Embed, linkpreview.Outcome) {
	f.calls.Add(1)
	title := "Page"
	return &linkpreview.Embed{URL: url, Type: "link", Title: &title},
		linkpreview.Outcome{Partial: true, Stage: "image", Reason: "timeout"}
}

func startPreviews(t *testing.T, e *env) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.msgs.RunPreviews(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })
}

func TestPreviewCreateEditAndCache(t *testing.T) {
	var calls atomic.Int32
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatalf("encode image: %v", err)
	}
	e := newEnv(t, func(o *messages.Options) {
		o.PreviewFetcher = previewFetchFunc(func(ctx context.Context, url string, save linkpreview.SaveImage) *linkpreview.Embed {
			calls.Add(1)
			src, err := save(ctx, img.Bytes(), "image/png")
			if err != nil {
				t.Errorf("save preview: %v", err)
				return nil
			}
			return &linkpreview.Embed{URL: url, Type: "image", Image: &linkpreview.Media{URL: src, Width: 2, Height: 3}}
		})
	})
	startPreviews(t, e)
	alice, _ := e.user("alice")
	g, general, _ := newGuild(t, alice, "home")
	const target = "https://example.test/img"
	resp := send(alice, general.ID, map[string]any{"content": target})
	expect(t, resp, http.StatusCreated)
	var created messages.PublicMessage
	resp.JSON(&created)
	if created.Embeds == nil || len(created.Embeds) != 0 {
		t.Fatalf("create embeds = %v, want []", created.Embeds)
	}
	update := waitPreview(t, e)
	if update.ID != created.ID || update.EditedAt != nil || len(update.Embeds) != 1 || update.Embeds[0].URL != target {
		t.Fatalf("first update = %+v, want original message with one preview", update)
	}
	src := update.Embeds[0].Image.URL
	got := alice.Do(http.MethodGet, src, nil)
	expect(t, got, http.StatusOK)
	tag := got.Header.Get("ETag")
	if got.Header.Get("Content-Type") != "image/png" || got.Header.Get("Cache-Control") != "private, no-cache" ||
		got.Header.Get("Vary") != "Cookie" || got.Header.Get("X-Content-Type-Options") != "nosniff" || tag == "" {
		t.Errorf("image headers = %v, want private PNG with revalidation", got.Header)
	}
	conditional := testkit.WithHeader("If-None-Match", tag)
	expect(t, alice.Do(http.MethodGet, src, nil, conditional), http.StatusNotModified)
	anonymous := testkit.NewClient(t, e.h)
	expect(t, anonymous.Do(http.MethodGet, src, nil), http.StatusNotFound)
	bob, bu := e.user("bob")
	e.join(g, bu)
	expect(t, bob.Do(http.MethodGet, src, nil), http.StatusOK)
	denyEveryone(t, alice, g, general.ID, permissions.ReadMessageHistory)
	expect(t, bob.Do(http.MethodGet, src, nil, conditional), http.StatusNotFound)
	// The same cached image is also usable from a group DM. Only its
	// recipients gain access, even when the guild copy remains inaccessible.
	var dmID int64
	err := e.pool.QueryRow(t.Context(), `
		WITH dm AS (
			INSERT INTO channels (id, type, owner_id, position, permission_synced, created_at)
			SELECT 100, 3, id, 0, false, now() FROM users WHERE username = 'alice'
			RETURNING id, owner_id
		), recipients AS (
			INSERT INTO channel_recipients (channel_id, user_id, joined_at)
			SELECT dm.id, id_of_recipient.id, now() FROM dm CROSS JOIN users AS id_of_recipient
			WHERE id_of_recipient.username IN ('alice', 'bob')
		)
		INSERT INTO messages (id, channel_id, author_id, content, created_at, embeds)
		SELECT 101, dm.id, dm.owner_id, m.content, now(), m.embeds
		FROM dm CROSS JOIN messages AS m WHERE m.id = $1
		RETURNING channel_id
	`, created.ID).Scan(&dmID)
	if err != nil {
		t.Fatalf("seed group preview: %v", err)
	}
	expect(t, bob.Do(http.MethodGet, src, nil, conditional), http.StatusNotModified)
	if _, err := e.pool.Exec(t.Context(), "DELETE FROM channel_recipients WHERE channel_id = $1 AND user_id = $2",
		dmID, parseID(t, bu.ID)); err != nil {
		t.Fatalf("remove group recipient: %v", err)
	}
	expect(t, bob.Do(http.MethodGet, src, nil, conditional), http.StatusNotFound)
	expect(t, alice.Do(http.MethodGet, "/api/v1/embeds/42", nil), http.StatusNotFound)
	resp = send(alice, general.ID, map[string]any{"content": target})
	expect(t, resp, http.StatusCreated)
	var cached messages.PublicMessage
	resp.JSON(&cached)
	if len(cached.Embeds) != 1 || calls.Load() != 1 {
		t.Fatalf("cached embeds = %v, calls = %d, want one each", cached.Embeds, calls.Load())
	}
	const added = "https://another.test/img"
	resp = alice.Do(http.MethodPatch, messagePath(general.ID, created.ID.String()), map[string]any{"content": added + " " + target})
	expect(t, resp, http.StatusOK)
	var withNew messages.PublicMessage
	resp.JSON(&withNew)
	if withNew.EditedAt == nil || len(withNew.Embeds) != 1 || withNew.Embeds[0].Image.URL != src {
		t.Fatalf("first edit = %+v, want retained cached image", withNew)
	}
	refreshed := waitPreviewAfter(t, e, 2)
	if len(refreshed.Embeds) != 2 || refreshed.Embeds[0].URL != added ||
		refreshed.Embeds[1].Image.URL != src || refreshed.EditedAt == nil || *refreshed.EditedAt != *withNew.EditedAt {
		t.Fatalf("refreshed edit = %+v, want new and retained previews in order with unchanged edit time", refreshed)
	}
	resp = alice.Do(http.MethodPatch, messagePath(general.ID, created.ID.String()), map[string]any{"content": "<" + target + ">"})
	expect(t, resp, http.StatusOK)
	var edited messages.PublicMessage
	resp.JSON(&edited)
	if edited.EditedAt == nil || edited.Embeds == nil || len(edited.Embeds) != 0 {
		t.Fatalf("edited message = %+v, want no preview and edit time", edited)
	}
}

func TestDeletedMessageDoesNotPublishPendingPreview(t *testing.T) {
	fetching := make(chan string, 4)
	release := make(chan struct{})
	const target = "https://deleted.test"
	const barrier = "https://barrier.test"
	e := newEnv(t, func(o *messages.Options) {
		o.PreviewFetcher = previewFetchFunc(func(ctx context.Context, url string, _ linkpreview.SaveImage) *linkpreview.Embed {
			if url == barrier {
				return &linkpreview.Embed{URL: url, Type: "link"}
			}
			fetching <- url
			if url == target {
				select {
				case <-ctx.Done():
				case <-release:
				}
				return &linkpreview.Embed{URL: url, Type: "link"}
			}
			<-ctx.Done()
			return nil
		})
	})
	startPreviews(t, e)
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")
	created := post(t, alice, general.ID, target)
	// Hold the three remaining workers so the barrier can be published only
	// after the worker fetching the deleted message has finished its job.
	for _, url := range []string{"https://busy1.test", "https://busy2.test", "https://busy3.test"} {
		post(t, alice, general.ID, url)
	}
	for range 4 {
		select {
		case <-fetching:
		case <-time.After(5 * time.Second):
			t.Fatal("preview fetch did not start")
		}
	}
	expect(t, alice.Do(http.MethodDelete, messagePath(general.ID, created.ID), nil), http.StatusNoContent)
	close(release)
	next := post(t, alice, general.ID, barrier)
	update := waitPreview(t, e)
	if update.ID.String() != next.ID || len(update.Embeds) != 1 || update.Embeds[0].URL != barrier {
		t.Errorf("update = %+v, want only the barrier message", update)
	}
}

func waitPreview(t *testing.T, e *env) messages.PublicMessage {
	t.Helper()
	return waitPreviewAfter(t, e, 0)
}

func waitPreviewAfter(t *testing.T, e *env, after int) messages.PublicMessage {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if updates := e.pub.ofType(events.MessageUpdate); len(updates) > after {
			return updates[after].Payload.(messages.PublicMessage)
		}
		select {
		case <-timer.C:
			t.Fatal("preview update did not arrive")
		case <-tick.C:
		}
	}
}

func TestPartialPreviewIsRetriedAfterAnHour(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	e := newEnv(t, func(o *messages.Options) {
		o.PreviewFetcher = partialFetch{&calls}
		o.Now = clock
	})
	startPreviews(t, e)
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")
	const target = "https://slow-picture.test/page"
	post(t, alice, general.ID, target)
	waitPreview(t, e)
	// Within the hour the card without its picture is served from the cache.
	post(t, alice, general.ID, target)
	if calls.Load() != 1 {
		t.Fatalf("fetches within the hour = %d, want 1", calls.Load())
	}
	mu.Lock()
	now = now.Add(61 * time.Minute)
	mu.Unlock()
	post(t, alice, general.ID, target)
	waitPreviewAfter(t, e, 1)
	if calls.Load() != 2 {
		t.Fatalf("fetches after the hour = %d, want 2", calls.Load())
	}
}
