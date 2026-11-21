package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// seedAgainst runs `restore-check seed` against base and returns the state it
// printed.
func seedAgainst(t *testing.T, base string) []byte {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := runRestoreCheck(t.Context(), "seed", base, testkit.DefaultOrigin, nil, &out, &errOut); code != 0 {
		t.Fatalf("seed exit = %d\nstderr:\n%s", code, errOut.String())
	}
	return out.Bytes()
}

func TestRestoreCheckVerifiesWhatSeedCreated(t *testing.T) {
	api, _ := newAuthRouter(t)
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	state := seedAgainst(t, srv.URL)
	var out, errOut bytes.Buffer
	code := runRestoreCheck(t.Context(), "verify", srv.URL, testkit.DefaultOrigin, bytes.NewReader(state), &out, &errOut)
	if code != 0 {
		t.Fatalf("verify exit = %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errOut.String())
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); lines[len(lines)-1] != "OK" {
		t.Errorf("last line of stdout = %q, want OK", lines[len(lines)-1])
	}
}

func TestRestoreCheckSeedPrintsNoSession(t *testing.T) {
	api, _ := newAuthRouter(t)
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	state := seedAgainst(t, srv.URL)
	if !json.Valid(state) {
		t.Fatalf("seed stdout is not JSON: %s", state)
	}
	if strings.Contains(string(state), "zz_session") {
		t.Error("the state holds the session cookie")
	}
}

func TestRestoreCheckDetectsChangedImage(t *testing.T) {
	api, _ := newAuthRouter(t)
	var tamper atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tamper.Load() && strings.HasPrefix(r.URL.Path, "/api/v1/files/icons/") {
			_, _ = w.Write([]byte("not the icon"))
			return
		}
		api.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	state := seedAgainst(t, srv.URL)
	tamper.Store(true)
	var out, errOut bytes.Buffer
	code := runRestoreCheck(t.Context(), "verify", srv.URL, testkit.DefaultOrigin, bytes.NewReader(state), &out, &errOut)
	if code != 1 {
		t.Fatalf("verify exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "icon") {
		t.Errorf("stderr %q does not name the icon", errOut.String())
	}
}

func TestRestoreCheckDetectsChangedAttachmentAndEmoji(t *testing.T) {
	for _, tc := range []struct{ name, path, label string }{
		{"attachment", "/api/v1/attachments/", "attachment"},
		{"emoji", "/api/v1/emojis/", "emoji"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, _ := newAuthRouter(t)
			var tamper atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tamper.Load() && strings.HasPrefix(r.URL.Path, tc.path) {
					_, _ = w.Write([]byte("unexpected bytes"))
					return
				}
				api.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)
			state := seedAgainst(t, srv.URL)
			tamper.Store(true)
			var out, stderr bytes.Buffer
			code := runRestoreCheck(t.Context(), "verify", srv.URL, testkit.DefaultOrigin, bytes.NewReader(state), &out, &stderr)
			if code != 1 || !strings.Contains(stderr.String(), tc.label) {
				t.Fatalf("verify = %d, %q; want exit 1 and step %q", code, stderr.String(), tc.label)
			}
		})
	}
}

func TestRestoreCheckRejectsBrokenState(t *testing.T) {
	var errOut bytes.Buffer
	code := runRestoreCheck(t.Context(), "verify", "http://127.0.0.1:1", testkit.DefaultOrigin,
		strings.NewReader("{"), &bytes.Buffer{}, &errOut)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "state") {
		t.Errorf("stderr %q does not explain the state", errOut.String())
	}
}

func TestRestoreCheckCommandUsage(t *testing.T) {
	for _, args := range [][]string{
		{"restore-check"},
		{"restore-check", "seed"},
		{"restore-check", "check", "http://127.0.0.1:1"},
		{"restore-check", "seed", "ftp://x"},
	} {
		var errOut bytes.Buffer
		if code := run(args, smokeEnv(smokeTestOrigin), &bytes.Buffer{}, &errOut); code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, code)
		}
		if !strings.Contains(errOut.String(), "restore-check") {
			t.Errorf("%v: stderr %q has no usage", args, errOut.String())
		}
	}
}
