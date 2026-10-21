package auth

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// TestLoginVerifiesAHashOnEveryPath makes sure that the time a login takes
// does not tell whether the account exists or is disabled: every path runs
// exactly one full Argon2id verification with the production parameters.
func TestLoginVerifiesAHashOnEveryPath(t *testing.T) {
	svc := NewService(Options{Pool: testkit.Postgres(t), IDs: snowflake.NewGenerator(0, time.Now)})
	var hashes []string
	svc.verify = func(pw, encoded string) (bool, bool, error) {
		hashes = append(hashes, encoded)
		return VerifyPassword(pw, encoded)
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

func TestClientKeyStripsZone(t *testing.T) {
	svc := &Service{}
	key := func(remote string) string {
		r := httptest.NewRequest("POST", "/x", nil)
		r.RemoteAddr = remote
		return svc.clientKey(r)
	}
	if a, b := key("[fe80::1%eth0]:5000"), key("[fe80::1%25eth1]:5000"); a != b || a != "fe80::1" {
		t.Fatalf("keys = %q, %q; want both fe80::1", a, b)
	}
	if k := key("192.0.2.1:4000"); k != "192.0.2.1" {
		t.Fatalf("key = %q", k)
	}
}
