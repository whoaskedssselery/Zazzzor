package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestCreateAdminFromStdin(t *testing.T) {
	url := testkit.PostgresURL(t)
	env := func(key string) string {
		return map[string]string{
			"ZAZZZOR_DATABASE_URL":    url,
			"ZAZZZOR_ALLOWED_ORIGINS": testkit.DefaultOrigin,
			"ZAZZZOR_ADMIN_USERNAMES": "boss",
		}[key]
	}
	for _, tc := range []struct {
		name, username, password, message string
		code                              int
	}{
		{"unlisted", "alice", "password123", "not listed", 1},
		{"invalid password", "boss", "bad", "invalid password", 1},
		{"configured username", "BOSS", "password123", "boss created", 0},
		{"already exists", "boss", "different-password", "username taken", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := createAdminCommand([]string{tc.username}, env, strings.NewReader(tc.password+"\n"), &out, &stderr)
			if got := out.String() + stderr.String(); code != tc.code || !strings.Contains(got, tc.message) || strings.Contains(got, tc.password) {
				t.Fatalf("exit = %d, output = %q; want exit %d and %q without the password", code, got, tc.code, tc.message)
			}
		})
	}
}

func TestCreateAdminRequiresOnlyUsernameArgument(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"create-admin", "boss", "extra"}, func(string) string { return "" }, io.Discard, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "create-admin <username>") {
		t.Fatalf("exit = %d, stderr = %q; want usage for create-admin <username>", code, stderr.String())
	}
}
