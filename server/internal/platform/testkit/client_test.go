package testkit

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

// recorder is a testing.TB that records failures instead of failing the real
// test. Fatal* stops the calling goroutine, like the real thing.
type recorder struct {
	testing.TB

	mu     sync.Mutex
	failed bool
	msgs   []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	r.FailNow()
}

func (r *recorder) Fatal(args ...any) {
	r.Errorf("%s", fmt.Sprint(args...))
	r.FailNow()
}

func (r *recorder) Error(args ...any) { r.Errorf("%s", fmt.Sprint(args...)) }

func (r *recorder) Fail() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = true
}

func (r *recorder) FailNow() {
	r.Fail()
	runtime.Goexit()
}

func (r *recorder) Failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failed
}

func (r *recorder) messages() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.msgs, "\n")
}

// run calls fn with a recorder in its own goroutine so Fatal can Goexit.
func run(t *testing.T, fn func(tb testing.TB)) *recorder {
	t.Helper()
	rec := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(rec)
	}()
	<-done
	return rec
}

func contractHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/readyz", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "problem":
			httpx.WriteProblem(w, httpx.ServiceUnavailable("not_ready"))
		case "bad-problem":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"type":"oops","title":"x","status":503,"code":"not_ready"}`))
		case "extra-field":
			httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok", "secret": "x"})
		case "wrong-const":
			httpx.JSON(w, http.StatusOK, map[string]string{"status": "nope"})
		case "wrong-type":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("ok"))
		case "teapot":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
	})
	mux.HandleFunc("GET /api/v1/not-in-contract", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		// Echo what the client sent so tests can inspect it.
		w.Header().Set("X-Seen-Origin", r.Header.Get("Origin"))
		httpx.WriteProblem(w, httpx.MethodNotAllowed())
	})
	return mux
}

func TestClientAcceptsResponseMatchingContract(t *testing.T) {
	c := NewClient(t, contractHandler())
	resp := c.Do(http.MethodGet, "/api/v1/healthz", nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d", resp.Status)
	}
	var body struct{ Status string }
	resp.JSON(&body)
	if body.Status != "ok" {
		t.Fatalf("body = %s", resp.Body)
	}
	// problem+json bodies that match the Problem schema pass too.
	if resp := c.Do(http.MethodGet, "/api/v1/readyz?mode=problem", nil); resp.Status != http.StatusServiceUnavailable {
		t.Fatalf("problem: status = %d", resp.Status)
	}
}

func TestClientRejectsResponseNotInContract(t *testing.T) {
	rec := run(t, func(tb testing.TB) {
		NewClient(tb, contractHandler()).Do(http.MethodGet, "/api/v1/not-in-contract", nil)
	})
	if !rec.Failed() {
		t.Fatal("a path outside the contract did not fail the test")
	}
	if !strings.Contains(rec.messages(), "/api/v1/not-in-contract") {
		t.Errorf("failure message does not name the request: %s", rec.messages())
	}
}

func TestClientRejectsResponsesThatBreakTheSchema(t *testing.T) {
	for _, mode := range []string{"extra-field", "wrong-const", "wrong-type", "teapot", "bad-problem"} {
		t.Run(mode, func(t *testing.T) {
			rec := run(t, func(tb testing.TB) {
				NewClient(tb, contractHandler()).Do(http.MethodGet, "/api/v1/readyz?mode="+mode, nil)
			})
			if !rec.Failed() {
				t.Fatalf("mode %s: response passed validation", mode)
			}
			t.Log(rec.messages())
		})
	}
}

func TestClientRejectsUnknownMethod(t *testing.T) {
	rec := run(t, func(tb testing.TB) {
		NewClient(tb, contractHandler()).Do(http.MethodPost, "/api/v1/healthz", nil)
	})
	if !rec.Failed() {
		t.Fatal("a method outside the contract did not fail the test")
	}
}

func TestClientSendsOriginAndKeepsCookies(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, fmt.Sprintf("origin=%q cookie=%q", r.Header.Get("Origin"), r.Header.Get("Cookie")))
		mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "zz_session", Value: "abc", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	c := NewClient(t, mux)
	c.Do(http.MethodGet, "/api/v1/healthz", nil)
	c.Do(http.MethodGet, "/api/v1/healthz", nil, WithOrigin("https://evil.example"))
	c.Do(http.MethodGet, "/api/v1/healthz", nil, WithoutOrigin())
	c.Origin = "https://other.test"
	c.Do(http.MethodGet, "/api/v1/healthz", nil)

	want := []string{
		`origin="https://zazzzor.test" cookie=""`,
		`origin="https://evil.example" cookie="zz_session=abc"`,
		`origin="" cookie="zz_session=abc"`,
		`origin="https://other.test" cookie="zz_session=abc"`,
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("server saw:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

func TestClientEncodesJSONBodies(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			httpx.WriteProblem(w, httpx.UnsupportedMediaType(ct))
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	c := NewClient(t, mux)
	if resp := c.Do(http.MethodGet, "/api/v1/healthz", map[string]int{"a": 1}); resp.Status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.Status, resp.Body)
	}
}
