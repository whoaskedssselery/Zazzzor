package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestRegisterLoginLogout(t *testing.T) {
	e := newEnv(t)
	c := e.client()

	u := register(t, c, "alice", "password123")
	if u.Username != "alice" || u.DisplayName != "alice" || u.AvatarURL != nil || u.ID == "" {
		t.Fatalf("registered user = %+v", u)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", u.CreatedAt); err != nil {
		t.Errorf("created_at %q is not RFC 3339 UTC with milliseconds", u.CreatedAt)
	}

	me := c.Do(http.MethodGet, "/api/v1/users/@me", nil)
	if me.Status != http.StatusOK {
		t.Fatalf("GET @me after register = %d: %s", me.Status, me.Body)
	}
	var got userBody
	me.JSON(&got)
	if got != u {
		t.Fatalf("@me = %+v, want %+v", got, u)
	}

	out := c.Do(http.MethodPost, "/api/v1/auth/logout", nil)
	if out.Status != http.StatusNoContent {
		t.Fatalf("logout = %d: %s", out.Status, out.Body)
	}
	if ck := sessionCookie(t, out); ck.MaxAge >= 0 || ck.Value != "" {
		t.Errorf("logout cookie = %+v, want deleted", ck)
	}
	problemOf(t, c.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")

	// Logging back in issues a new working session.
	in := c.Do(http.MethodPost, "/api/v1/auth/login", creds("alice", "password123"))
	if in.Status != http.StatusOK {
		t.Fatalf("login = %d: %s", in.Status, in.Body)
	}
	var logged userBody
	in.JSON(&logged)
	if logged != u {
		t.Fatalf("login user = %+v, want %+v", logged, u)
	}
	if me := c.Do(http.MethodGet, "/api/v1/users/@me", nil); me.Status != http.StatusOK {
		t.Fatalf("GET @me after login = %d", me.Status)
	}
	// A second logout without a session is still 204.
	c.Do(http.MethodPost, "/api/v1/auth/logout", nil)
	if out := c.Do(http.MethodPost, "/api/v1/auth/logout", nil); out.Status != http.StatusNoContent {
		t.Fatalf("logout without session = %d", out.Status)
	}
}

func TestRegisterNormalizesAndValidates(t *testing.T) {
	e := newEnv(t)
	c := e.client()

	resp := c.Do(http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": "Vasya", "password": "password123", "display_name": "  Вася Пупкин  "})
	if resp.Status != http.StatusCreated {
		t.Fatalf("register Vasya = %d: %s", resp.Status, resp.Body)
	}
	var u userBody
	resp.JSON(&u)
	if u.Username != "vasya" || u.DisplayName != "Вася Пупкин" {
		t.Fatalf("user = %+v, want username vasya, display name trimmed", u)
	}
	var stored string
	if err := e.pool.QueryRow(t.Context(), "SELECT username FROM users WHERE id = $1::text::bigint", u.ID).Scan(&stored); err != nil || stored != "vasya" {
		t.Fatalf("stored username = %q (%v)", stored, err)
	}

	for _, name := range []string{"в", "a b", strings.Repeat("a", 33), "a", "", "Kelvin", "bob!"} {
		p := problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/register", creds(name, "password123")),
			http.StatusBadRequest, "validation_failed")
		if len(p.Errors) == 0 || p.Errors[0].Field != "username" {
			t.Errorf("username %q: errors = %+v, want username first", name, p.Errors)
		}
	}
	for _, pw := range []string{"1234567", strings.Repeat("я", 129)} {
		p := problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/register", creds("okname", pw)),
			http.StatusBadRequest, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != "password" {
			t.Errorf("password of %d runes: errors = %+v", len([]rune(pw)), p.Errors)
		}
	}
	// 8 and 128 runes (multi-byte) are accepted.
	register(t, c, "eight", "12345678")
	if resp := c.Do(http.MethodPost, "/api/v1/auth/register", creds("long_pw", strings.Repeat("я", 128))); resp.Status != http.StatusCreated {
		t.Fatalf("128-rune password = %d: %s", resp.Status, resp.Body)
	}

	for _, dn := range []string{"   ", strings.Repeat("ж", 33), "a\x00b"} {
		p := problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/register",
			map[string]string{"username": "dname", "password": "password123", "display_name": dn}),
			http.StatusBadRequest, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != "display_name" {
			t.Errorf("display name %q: errors = %+v", dn, p.Errors)
		}
	}

	// All invalid fields are reported at once.
	p := problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/register", creds("x y", "short")),
		http.StatusBadRequest, "validation_failed")
	if len(p.Errors) != 2 || p.Errors[0].Field != "username" || p.Errors[1].Field != "password" {
		t.Errorf("errors = %+v, want username then password", p.Errors)
	}

	problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/register", `{"username":"zz","password":"password123","admin":true}`),
		http.StatusBadRequest, "invalid_body")
	problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/register", "x", testkit.WithHeader("Content-Type", "text/plain")),
		http.StatusUnsupportedMediaType, "unsupported_media_type")
}

func TestRegisterDuplicate(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	register(t, c, "bob", "password123")
	for _, name := range []string{"BOB", "Bob"} {
		problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/register", creds(name, "otherpass1")),
			http.StatusConflict, "username_taken")
	}
}

func TestRegisterClosed(t *testing.T) {
	open := false
	e := newEnv(t, func(o *auth.Options) {
		o.RegistrationOpen = func(context.Context) (bool, error) { return open, nil }
	})
	c := e.client()
	problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/register", creds("carol", "password123")),
		http.StatusForbidden, "registration_closed")
	open = true
	register(t, c, "carol", "password123")
}

func TestLoginSameErrorForUnknownAndWrongPassword(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	register(t, c, "dave", "password123")

	wrong := c.Do(http.MethodPost, "/api/v1/auth/login", creds("dave", "password124"))
	unknown := c.Do(http.MethodPost, "/api/v1/auth/login", creds("nobody", "password123"))
	malformed := c.Do(http.MethodPost, "/api/v1/auth/login", creds("не логин", "password123"))
	for name, resp := range map[string]*testkit.Response{"wrong": wrong, "unknown": unknown, "malformed": malformed} {
		problemOf(t, resp, http.StatusUnauthorized, "invalid_credentials")
		if len(resp.Header.Values("Set-Cookie")) != 0 {
			t.Errorf("%s: failed login set a cookie", name)
		}
	}
	if !bytes.Equal(wrong.Body, unknown.Body) || !bytes.Equal(wrong.Body, malformed.Body) {
		t.Fatalf("bodies differ:\nwrong:   %s\nunknown: %s\nmalformed: %s", wrong.Body, unknown.Body, malformed.Body)
	}
	// Login is case-insensitive on the username.
	if resp := c.Do(http.MethodPost, "/api/v1/auth/login", creds("DAVE", "password123")); resp.Status != http.StatusOK {
		t.Fatalf("login DAVE = %d: %s", resp.Status, resp.Body)
	}
}

func TestLoginDisabledAccount(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	u := register(t, c, "erin", "password123")
	if _, err := e.pool.Exec(t.Context(), "UPDATE users SET disabled_at = now() WHERE id = $1::text::bigint", u.ID); err != nil {
		t.Fatal(err)
	}
	// The session issued before the account was disabled no longer works.
	problemOf(t, c.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	// A wrong password does not reveal that the account is disabled.
	problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/login", creds("erin", "wrongpass1")),
		http.StatusUnauthorized, "invalid_credentials")
	problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/login", creds("erin", "password123")),
		http.StatusForbidden, "account_disabled")
}

func TestLoginRateLimited(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	register(t, c, "frank", "password123")
	for i := range 10 {
		resp := c.Do(http.MethodPost, "/api/v1/auth/login", creds("frank", "wrong-"+strconv.Itoa(i)))
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, resp.Status)
		}
	}
	resp := c.Do(http.MethodPost, "/api/v1/auth/login", creds("frank", "password123"))
	problemOf(t, resp, http.StatusTooManyRequests, "rate_limited")
	secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || secs < 1 || secs > 6 {
		t.Fatalf("Retry-After = %q, want 1..6 seconds", resp.Header.Get("Retry-After"))
	}
}

func TestRegisterRateLimited(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	for i := range 3 {
		register(t, c, "user"+strconv.Itoa(i), "password123")
	}
	// Invalid requests are refused before the limiter and do not count.
	problemOf(t, c.Do(http.MethodPost, "/api/v1/auth/register", creds("no way", "password123")),
		http.StatusBadRequest, "validation_failed")
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", creds("user4", "password123"))
	problemOf(t, resp, http.StatusTooManyRequests, "rate_limited")
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err != nil || secs < 1 || secs > 20*60 {
		t.Fatalf("Retry-After = %q, want 1..1200 seconds", resp.Header.Get("Retry-After"))
	}
}

func TestRateLimitKeyedByForwardedClient(t *testing.T) {
	e := newEnv(t, func(o *auth.Options) {
		o.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
		o.LoginLimiter = ratelimit.New(rate.Every(time.Hour), 1, time.Hour)
	})
	c := e.client()
	login := func(xff string) int {
		return c.Do(http.MethodPost, "/api/v1/auth/login", creds("ghost", "password123"),
			testkit.WithHeader("X-Forwarded-For", xff)).Status
	}
	if s := login("fe80::1%eth0"); s != http.StatusUnauthorized {
		t.Fatalf("first = %d", s)
	}
	// The same address with another zone is the same client.
	if s := login("fe80::1%eth1"); s != http.StatusTooManyRequests {
		t.Fatalf("zoned twin = %d, want 429", s)
	}
	if s := login("203.0.113.9"); s != http.StatusUnauthorized {
		t.Fatalf("other client = %d, want 401", s)
	}
}

func TestSessionStoredHashed(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", creds("gina", "password123"))
	token := sessionCookie(t, resp).Value
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token is %d bytes (%v), want 32 bytes base64url", len(raw), err)
	}

	var stored []byte
	var ua *string
	var ip *string
	if err := e.pool.QueryRow(t.Context(),
		"SELECT token_hash, user_agent, host(ip) FROM sessions").Scan(&stored, &ua, &ip); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(token))
	if !bytes.Equal(stored, want[:]) {
		t.Fatalf("token_hash = %x, want SHA-256 of the cookie value", stored)
	}
	for _, form := range [][]byte{[]byte(token), raw, []byte(hex.EncodeToString(raw))} {
		if bytes.Contains(stored, form) {
			t.Fatal("token_hash contains the raw token")
		}
	}
	if ip == nil || *ip != "127.0.0.1" {
		t.Errorf("session ip = %v, want 127.0.0.1", ip)
	}
	if ua == nil || !strings.Contains(*ua, "Go-http-client") {
		t.Errorf("session user_agent = %v", ua)
	}
}

func TestCookieAttributes(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	for _, resp := range []*testkit.Response{
		c.Do(http.MethodPost, "/api/v1/auth/register", creds("hank", "password123")),
		c.Do(http.MethodPost, "/api/v1/auth/login", creds("hank", "password123")),
	} {
		ck := sessionCookie(t, resp)
		if !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" {
			t.Errorf("cookie = %+v, want HttpOnly, Secure, SameSite=Lax, Path=/", ck)
		}
		if ck.MaxAge != 30*24*60*60 {
			t.Errorf("Max-Age = %d, want 30 days", ck.MaxAge)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", resp.Header.Get("Cache-Control"))
		}
	}

	insecure := newEnv(t, func(o *auth.Options) { o.CookieSecure = false })
	resp := insecure.client().Do(http.MethodPost, "/api/v1/auth/register", creds("ivan", "password123"))
	if sessionCookie(t, resp).Secure {
		t.Error("CookieSecure=false still set Secure")
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	register(t, c, "judy", "password123")
	if _, err := e.pool.Exec(t.Context(), "UPDATE sessions SET expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	resp := c.Do(http.MethodGet, "/api/v1/users/@me", nil)
	problemOf(t, resp, http.StatusUnauthorized, "unauthenticated")

	// The same holds when the clock passes the expiry naturally.
	register(t, c, "judy2", "password123")
	e.clock.Advance(30*24*time.Hour + time.Minute)
	problemOf(t, c.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")

	// A garbage cookie is simply no session.
	c2 := e.client()
	problemOf(t, c2.Do(http.MethodGet, "/api/v1/users/@me", nil, testkit.WithHeader("Cookie", "zz_session=garbage")),
		http.StatusUnauthorized, "unauthenticated")
}

func TestSessionRenewedAtMostDaily(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	register(t, c, "kate", "password123")
	expiry := func() time.Time {
		var at time.Time
		if err := e.pool.QueryRow(t.Context(), "SELECT expires_at FROM sessions").Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	first := expiry()

	e.clock.Advance(23 * time.Hour)
	resp := c.Do(http.MethodGet, "/api/v1/users/@me", nil)
	if resp.Status != http.StatusOK || !expiry().Equal(first) || len(resp.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("renewed within a day: status %d, expiry %v -> %v, cookies %v",
			resp.Status, first, expiry(), resp.Header.Values("Set-Cookie"))
	}

	e.clock.Advance(2 * time.Hour)
	resp = c.Do(http.MethodGet, "/api/v1/users/@me", nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("@me = %d", resp.Status)
	}
	if got := expiry(); got.Sub(first) < 24*time.Hour {
		t.Fatalf("expiry %v -> %v, want moved by ~25h", first, got)
	}
	if ck := sessionCookie(t, resp); ck.MaxAge != 30*24*60*60 {
		t.Fatalf("renewal cookie Max-Age = %d", ck.MaxAge)
	}

	// 29 days later the renewed session is still alive (it would have
	// expired without renewal).
	e.clock.Advance(29 * 24 * time.Hour)
	if resp := c.Do(http.MethodGet, "/api/v1/users/@me", nil); resp.Status != http.StatusOK {
		t.Fatalf("renewed session rejected: %d", resp.Status)
	}
}

func TestLogoutPublishesSessionRevoked(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	u := register(t, c, "liam", "password123")
	c.Do(http.MethodPost, "/api/v1/auth/logout", nil)

	evs := e.pub.all()
	if len(evs) != 1 || evs[0].Type != events.SessionRevoked {
		t.Fatalf("events = %+v, want one SESSION_REVOKED", evs)
	}
	p, ok := evs[0].Payload.(events.SessionRevokedPayload)
	if !ok || p.UserID.String() != u.ID || len(p.SessionIDs) != 1 {
		t.Fatalf("payload = %#v", evs[0].Payload)
	}
	if a := evs[0].Audience; a.Kind != events.AudienceUsers || len(a.UserIDs) != 1 || a.UserIDs[0] != p.UserID {
		t.Fatalf("audience = %+v", a)
	}
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM sessions").Scan(&n); err != nil || n != 0 {
		t.Fatalf("sessions left = %d (%v)", n, err)
	}
}

func TestLogsCarryNoSecrets(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	const pw = "s3cret-Pa55word"
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", creds("mona", pw))
	token := sessionCookie(t, resp).Value
	c.Do(http.MethodPost, "/api/v1/auth/login", creds("mona", "wrong-"+pw))
	c.Do(http.MethodPost, "/api/v1/auth/login", creds("mona", pw))
	c.Do(http.MethodGet, "/api/v1/users/@me", nil)
	logs := e.logs.String()
	for _, secret := range []string{pw, token} {
		if strings.Contains(logs, secret) {
			t.Fatalf("logs contain a secret: %s", logs)
		}
	}
}
