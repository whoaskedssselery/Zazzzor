package users_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/users"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

type recordingPublisher struct {
	mu     sync.Mutex
	events []events.Event
}

func (p *recordingPublisher) Publish(_ context.Context, e events.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
}

func (p *recordingPublisher) ofType(typ string) []events.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []events.Event
	for _, e := range p.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

type env struct {
	t    *testing.T
	auth *auth.Service
	disk *files.Disk
	dir  string
	pub  *recordingPublisher
	h    http.Handler
}

func newEnv(t *testing.T, opts ...func(*users.Options)) *env {
	t.Helper()
	e := &env{t: t, pub: &recordingPublisher{}, dir: t.TempDir()}
	e.auth = auth.NewService(auth.Options{
		Pool:            testkit.Postgres(t),
		IDs:             snowflake.NewGenerator(0, time.Now),
		Publisher:       e.pub,
		CookieSecure:    true,
		RegisterLimiter: ratelimit.New(rate.Inf, 1, time.Minute),
	})
	var err error
	if e.disk, err = files.NewDisk(e.dir); err != nil {
		t.Fatal(err)
	}
	o := users.Options{Accounts: e.auth, Files: e.disk, Publisher: e.pub}
	for _, opt := range opts {
		opt(&o)
	}
	svc := users.NewService(o)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithLogger(r.Context(), logger)))
		})
	})
	r.Use(httpx.RequireOrigin([]string{testkit.DefaultOrigin}))
	r.Route("/api/v1", func(r chi.Router) {
		files.Mount(r, e.disk)
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(e.auth))
			auth.Mount(r, e.auth)
			users.Mount(r, svc)
		})
	})
	e.h = r
	return e
}

func (e *env) client() *testkit.Client {
	e.t.Helper()
	return testkit.NewClient(e.t, e.h)
}

type userBody struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	CreatedAt   string  `json:"created_at"`
}

type problem struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Errors []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

func creds(username, password string) map[string]string {
	return map[string]string{"username": username, "password": password}
}

func register(t *testing.T, c *testkit.Client, username, password string) userBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", creds(username, password))
	if resp.Status != http.StatusCreated {
		t.Fatalf("register %s: status %d: %s", username, resp.Status, resp.Body)
	}
	var u userBody
	resp.JSON(&u)
	return u
}

func login(t *testing.T, c *testkit.Client, username, password string) {
	t.Helper()
	if resp := c.Do(http.MethodPost, "/api/v1/auth/login", creds(username, password)); resp.Status != http.StatusOK {
		t.Fatalf("login %s: status %d: %s", username, resp.Status, resp.Body)
	}
}

func me(t *testing.T, c *testkit.Client) userBody {
	t.Helper()
	resp := c.Do(http.MethodGet, "/api/v1/users/@me", nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("GET @me: status %d: %s", resp.Status, resp.Body)
	}
	var u userBody
	resp.JSON(&u)
	return u
}

func problemOf(t *testing.T, resp *testkit.Response, status int, code string) problem {
	t.Helper()
	if resp.Status != status {
		t.Fatalf("status = %d, want %d: %s", resp.Status, status, resp.Body)
	}
	var p problem
	resp.JSON(&p)
	if p.Code != code {
		t.Fatalf("code = %q, want %q", p.Code, code)
	}
	return p
}

func userID(t *testing.T, u userBody) snowflake.ID {
	t.Helper()
	id, err := snowflake.Parse(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// multipartBody wraps content as the part "file" (or field) of a form and
// returns the body and its Content-Type.
func multipartBody(t *testing.T, field, filename, partType string, content []byte) ([]byte, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="` + field + `"; filename="` + filename + `"`}
	h["Content-Type"] = []string{partType}
	part, err := w.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), w.FormDataContentType()
}

func uploadAvatar(t *testing.T, c *testkit.Client, filename, partType string, content []byte) *testkit.Response {
	t.Helper()
	body, ct := multipartBody(t, "file", filename, partType, content)
	return c.Do(http.MethodPut, "/api/v1/users/@me/avatar", body, testkit.WithHeader("Content-Type", ct))
}

func photo(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
