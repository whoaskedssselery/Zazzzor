package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// TestLoginVerifiesAHashOnEveryPath makes sure that the time a login takes
// does not tell whether the account exists or is disabled: every path runs
// exactly one full Argon2id verification with the production parameters.
func TestLoginVerifiesAHashOnEveryPath(t *testing.T) {
	svc := NewService(Options{Pool: testkit.Postgres(t), IDs: snowflake.NewGenerator(0, time.Now)})
	var hashes []string
	svc.verify = func(ctx context.Context, pw, encoded string) (bool, bool, error) {
		hashes = append(hashes, encoded)
		return VerifyPassword(ctx, pw, encoded)
	}
	ctx := t.Context()
	if _, _, err := svc.Register(ctx, RegisterInput{Username: "tess", Password: "password123"}, SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.pool.Exec(ctx, "UPDATE users SET disabled_at = now()"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, username, password string
		want                     error
	}{
		{"unknown user", "nobody", "password123", ErrInvalidCredentials},
		{"malformed username", "a b", "password123", ErrInvalidCredentials},
		{"disabled, wrong password", "tess", "nope-nope", ErrInvalidCredentials},
		{"disabled, right password", "tess", "password123", ErrAccountDisabled},
	}
	for _, tc := range cases {
		hashes = hashes[:0]
		_, _, err := svc.Login(ctx, tc.username, tc.password, SessionMeta{})
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if len(hashes) != 1 {
			t.Fatalf("%s: %d verifications, want 1", tc.name, len(hashes))
		}
		if p, err := parsePHC(hashes[0]); err != nil || p.params != currentParams {
			t.Fatalf("%s: verified against %q, want a hash with the production parameters", tc.name, hashes[0])
		}
	}
}

func TestClientKeyGroupsIPv6By64(t *testing.T) {
	svc := &Service{}
	key := func(remote string) string {
		r := httptest.NewRequest(http.MethodPost, "/x", nil)
		r.RemoteAddr = remote
		return svc.clientKey(r)
	}
	for remote, want := range map[string]string{
		"[fe80::1%eth0]:5000":            "fe80::/64",
		"[fe80::1%eth1]:5000":            "fe80::/64",
		"[2001:db8:1:2::1]:5000":         "2001:db8:1:2::/64",
		"[2001:db8:1:2:ffff:1:2:3]:5000": "2001:db8:1:2::/64",
		"[2001:db8:1:3::1]:5000":         "2001:db8:1:3::/64",
		"192.0.2.1:4000":                 "192.0.2.1",
		"[::ffff:192.0.2.1]:4000":        "192.0.2.1",
	} {
		if got := key(remote); got != want {
			t.Errorf("clientKey(%s) = %q, want %q", remote, got, want)
		}
	}
	// The session keeps the full address (without the zone).
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.RemoteAddr = "[2001:db8:1:2::1%eth0]:5000"
	if ip := svc.sessionMeta(r).IP; ip != netip.MustParseAddr("2001:db8:1:2::1") {
		t.Fatalf("session IP = %v", ip)
	}
}

// holdHashSlots occupies every hashing slot until the test ends.
func holdHashSlots(t *testing.T) {
	t.Helper()
	for range maxConcurrentHashes {
		hashSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for range maxConcurrentHashes {
			<-hashSlots
		}
	})
}

func TestHashingSkipsCancelledRequests(t *testing.T) {
	enc, err := HashPassword(t.Context(), "password123")
	if err != nil {
		t.Fatal(err)
	}
	holdHashSlots(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	if _, err := HashPassword(ctx, "password123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("HashPassword(cancelled) err = %v", err)
	}
	if _, _, err := VerifyPassword(ctx, "password123", enc); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyPassword(cancelled) err = %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("cancelled calls took %v, want prompt", d)
	}

	// A request cancelled while it waits gives up at once, too.
	ctx, cancel = context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)
	start = time.Now()
	if _, err := HashPassword(ctx, "password123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("HashPassword(cancelled while waiting) err = %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("waiting call returned after %v", d)
	}
}

func TestHashingTimesOutWhenBusy(t *testing.T) {
	old := hashWaitTimeout
	if old != 3*time.Second {
		t.Fatalf("hashWaitTimeout = %v, want 3s", old)
	}
	hashWaitTimeout = 100 * time.Millisecond
	t.Cleanup(func() { hashWaitTimeout = old })

	svc := NewService(Options{Pool: testkit.Postgres(t), IDs: snowflake.NewGenerator(0, time.Now), CookieSecure: true})
	api := chi.NewRouter()
	api.Use(Middleware(svc))
	Mount(api, svc)
	root := chi.NewRouter()
	root.Mount("/api/v1", api)
	c := testkit.NewClient(t, root)

	holdHashSlots(t)
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/register"} {
		resp := c.Do(http.MethodPost, path, map[string]string{"username": "busy", "password": "password123"})
		if resp.Status != http.StatusServiceUnavailable {
			t.Fatalf("%s = %d, want 503: %s", path, resp.Status, resp.Body)
		}
		var p struct{ Code string }
		resp.JSON(&p)
		if p.Code != "server_busy" || resp.Header.Get("Retry-After") != "5" {
			t.Fatalf("%s: code %q, Retry-After %q; want server_busy, 5", path, p.Code, resp.Header.Get("Retry-After"))
		}
	}
	if err := svc.ChangePassword(t.Context(), 1, "password123", "password456", 0); !errors.Is(err, ErrBusy) {
		t.Fatalf("ChangePassword while busy: err = %v, want ErrBusy", err)
	}
}
