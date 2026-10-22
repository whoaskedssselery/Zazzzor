package main

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// The production wiring: the registration switch of the admin module gates
// auth's registration.
func TestRegistrationFollowsInstanceSetting(t *testing.T) {
	h, _ := newAuthRouter(t)
	boss := testkit.NewClient(t, h)
	creds := func(name string) map[string]string {
		return map[string]string{"username": name, "password": "password123"}
	}
	if resp := boss.Do(http.MethodPost, "/api/v1/auth/register", creds("boss")); resp.Status != http.StatusCreated {
		t.Fatalf("register boss = %d", resp.Status)
	}
	if resp := boss.Do(http.MethodPatch, "/api/v1/admin/settings", map[string]bool{"registration_open": false}); resp.Status != http.StatusOK {
		t.Fatalf("close registration = %d: %s", resp.Status, resp.Body)
	}
	guest := testkit.NewClient(t, h)
	resp := guest.Do(http.MethodPost, "/api/v1/auth/register", creds("guest"))
	if resp.Status != http.StatusForbidden || !strings.Contains(string(resp.Body), `"registration_closed"`) {
		t.Fatalf("register while closed = %d: %s", resp.Status, resp.Body)
	}
	if resp := boss.Do(http.MethodPatch, "/api/v1/admin/settings", map[string]bool{"registration_open": true}); resp.Status != http.StatusOK {
		t.Fatalf("open registration = %d", resp.Status)
	}
	if resp := guest.Do(http.MethodPost, "/api/v1/auth/register", creds("guest")); resp.Status != http.StatusCreated {
		t.Fatalf("register while open = %d: %s", resp.Status, resp.Body)
	}
}

// slowReader hands out its data in two halves with a pause between them.
type slowReader struct {
	data  []byte
	pause time.Duration
	half  bool
}

func (r *slowReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	if !r.half {
		r.half = true
		n := copy(p[:min(len(p), len(r.data)/2)], r.data)
		r.data = r.data[n:]
		return n, nil
	}
	time.Sleep(r.pause)
	r.pause = 0
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// An avatar upload may take longer than the global body deadline; other
// requests may not.
func TestAvatarUploadOutlivesBodyDeadline(t *testing.T) {
	const deadline = 300 * time.Millisecond
	h, _ := newModulesRouter(t, httpx.DeadlineOptions{Read: deadline})
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	hc := srv.Client()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	hc.Jar = jar

	do := func(method, path, contentType string, body io.Reader) int {
		t.Helper()
		req, reqErr := http.NewRequestWithContext(t.Context(), method, srv.URL+path, body)
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		req.Header.Set("Origin", testkit.DefaultOrigin)
		req.Header.Set("Content-Type", contentType)
		resp, doErr := hc.Do(req)
		if doErr != nil {
			return -1 // the server cut the connection
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if s := do(http.MethodPost, "/api/v1/auth/register", "application/json",
		strings.NewReader(`{"username":"alice","password":"password123"}`)); s != http.StatusCreated {
		t.Fatalf("register = %d", s)
	}

	var img bytes.Buffer
	if err = png.Encode(&img, image.NewGray(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, err := mw.CreateFormFile("file", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(img.Bytes())
	_ = mw.Close()

	slow := func() io.Reader { return &slowReader{data: bytes.Clone(form.Bytes()), pause: 3 * deadline} }
	if s := do(http.MethodPut, "/api/v1/users/@me/avatar", mw.FormDataContentType(), slow()); s != http.StatusOK {
		t.Fatalf("slow avatar upload = %d, want 200", s)
	}
	// The same slow body on a JSON route runs into the global deadline.
	if s := do(http.MethodPatch, "/api/v1/users/@me", "application/json",
		&slowReader{data: []byte(`{"display_name":"Alice"}`), pause: 3 * deadline}); s == http.StatusOK {
		t.Fatalf("slow JSON body = %d, want the deadline to cut it", s)
	}
}

// The guild routes are served by the production wiring.
func TestGuildRoutesWired(t *testing.T) {
	h, _ := newAuthRouter(t)
	c := testkit.NewClient(t, h)
	if resp := c.Do(http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": "alice", "password": "password123"}); resp.Status != http.StatusCreated {
		t.Fatalf("register = %d", resp.Status)
	}
	if resp := c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": "home"}); resp.Status != http.StatusCreated {
		t.Fatalf("create guild = %d: %s", resp.Status, resp.Body)
	}
	resp := c.Do(http.MethodGet, "/api/v1/users/@me/guilds", nil)
	if resp.Status != http.StatusOK || !strings.Contains(string(resp.Body), `"name":"home"`) {
		t.Fatalf("my guilds = %d: %s", resp.Status, resp.Body)
	}
}

// Channels and invites are served by the production wiring, and a new
// guild gets its default channels.
func TestChannelAndInviteRoutesWired(t *testing.T) {
	h, _ := newAuthRouter(t)
	register := func(name string) *testkit.Client {
		c := testkit.NewClient(t, h)
		if resp := c.Do(http.MethodPost, "/api/v1/auth/register",
			map[string]string{"username": name, "password": "password123"}); resp.Status != http.StatusCreated {
			t.Fatalf("register %s = %d", name, resp.Status)
		}
		return c
	}
	owner, guest := register("alice"), register("bob")
	resp := owner.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": "home"})
	var g struct {
		ID string `json:"id"`
	}
	resp.JSON(&g)
	resp = owner.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/channels", nil)
	var cs []struct {
		ID   string `json:"id"`
		Type int    `json:"type"`
	}
	resp.JSON(&cs)
	if resp.Status != http.StatusOK || len(cs) != 4 || cs[1].Type != 0 {
		t.Fatalf("channels = %d: %s", resp.Status, resp.Body)
	}
	resp = owner.Do(http.MethodPost, "/api/v1/channels/"+cs[1].ID+"/invites", map[string]any{})
	var inv struct {
		Code string `json:"code"`
	}
	resp.JSON(&inv)
	if resp.Status != http.StatusCreated {
		t.Fatalf("create invite = %d: %s", resp.Status, resp.Body)
	}
	if resp = guest.Do(http.MethodPost, "/api/v1/invites/"+inv.Code, nil); resp.Status != http.StatusOK {
		t.Fatalf("join = %d: %s", resp.Status, resp.Body)
	}
}

// Messages are served by the production wiring.
func TestMessageRoutesWired(t *testing.T) {
	h, _ := newAuthRouter(t)
	c := testkit.NewClient(t, h)
	if resp := c.Do(http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": "alice", "password": "password123"}); resp.Status != http.StatusCreated {
		t.Fatalf("register = %d", resp.Status)
	}
	resp := c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": "home"})
	var g struct {
		ID string `json:"id"`
	}
	resp.JSON(&g)
	resp = c.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/channels", nil)
	var cs []struct {
		ID string `json:"id"`
	}
	resp.JSON(&cs)
	path := "/api/v1/channels/" + cs[1].ID + "/messages"
	if resp = c.Do(http.MethodPost, path, map[string]string{"content": "hello"}); resp.Status != http.StatusCreated {
		t.Fatalf("send = %d: %s", resp.Status, resp.Body)
	}
	resp = c.Do(http.MethodGet, path, nil)
	if resp.Status != http.StatusOK || !strings.Contains(string(resp.Body), `"content":"hello"`) {
		t.Fatalf("history = %d: %s", resp.Status, resp.Body)
	}
}
