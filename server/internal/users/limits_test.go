package users_test

import (
	"context"
	"errors"
	"image/color"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/users"
)

// failingAccounts makes SetAvatar fail on demand.
type failingAccounts struct {
	auth.Accounts
	fail atomic.Bool
}

func (f *failingAccounts) SetAvatar(ctx context.Context, id snowflake.ID, hash *string) (auth.User, *string, error) {
	if f.fail.Load() {
		return auth.User{}, nil, errors.New("database down")
	}
	return f.Accounts.SetAvatar(ctx, id, hash)
}

func storedAvatars(t *testing.T, e *env, u userBody) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(e.dir, "avatars", u.ID))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	return names
}

func TestAvatarFailedWriteLeavesNoFile(t *testing.T) {
	var fa *failingAccounts
	e := newEnv(t, func(o *users.Options) {
		fa = &failingAccounts{Accounts: o.Accounts}
		o.Accounts = fa
	})
	c := e.client()
	u := register(t, c, "alice", "password123")
	red := photo(t, 64, 64, color.RGBA{R: 200, A: 255})

	if resp := uploadAvatar(t, c, "a.jpg", "image/jpeg", red); resp.Status != http.StatusOK {
		t.Fatalf("first upload = %d", resp.Status)
	}
	current := storedAvatars(t, e, u)
	if len(current) != 1 {
		t.Fatalf("stored = %v", current)
	}

	fa.fail.Store(true)
	problemOf(t, uploadAvatar(t, c, "b.jpg", "image/jpeg", photo(t, 64, 64, color.RGBA{B: 200, A: 255})),
		http.StatusInternalServerError, "internal")
	if got := storedAvatars(t, e, u); len(got) != 1 || got[0] != current[0] {
		t.Fatalf("after a failed write: stored = %v, want only %v", got, current)
	}
	// A failed write of the image that is already current keeps its file.
	problemOf(t, uploadAvatar(t, c, "a.jpg", "image/jpeg", red), http.StatusInternalServerError, "internal")
	if got := storedAvatars(t, e, u); len(got) != 1 || got[0] != current[0] {
		t.Fatalf("after a failed same-image write: stored = %v, want %v", got, current)
	}
	fa.fail.Store(false)
	if m := me(t, c); m.AvatarURL == nil {
		t.Fatal("avatar lost")
	}
}

func TestProfileWritesRateLimited(t *testing.T) {
	e := newEnv(t, func(o *users.Options) { o.ProfileLimiter = nil }) // the real limit
	c := e.client()
	register(t, c, "alice", "password123")
	img := photo(t, 16, 16, color.White)
	writes := []func() int{
		func() int {
			return c.Do(http.MethodPatch, "/api/v1/users/@me", map[string]string{"display_name": "A"}).Status
		},
		func() int { return uploadAvatar(t, c, "a.jpg", "image/jpeg", img).Status },
		func() int { return c.Do(http.MethodDelete, "/api/v1/users/@me/avatar", nil).Status },
	}
	for i := range 10 {
		if s := writes[i%3](); s != http.StatusOK {
			t.Fatalf("write %d = %d", i+1, s)
		}
	}
	for i, w := range writes {
		if s := w(); s != http.StatusTooManyRequests {
			t.Fatalf("write %d after the limit = %d, want 429", i, s)
		}
	}
	resp := c.Do(http.MethodPatch, "/api/v1/users/@me", map[string]string{"display_name": "B"})
	problemOf(t, resp, http.StatusTooManyRequests, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	me(t, c) // reads are not limited
	other := e.client()
	register(t, other, "bob", "password123")
	if s := other.Do(http.MethodPatch, "/api/v1/users/@me", map[string]string{"display_name": "Bob"}).Status; s != http.StatusOK {
		t.Fatalf("other user = %d", s)
	}
}
