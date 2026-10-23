package testkit

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
)

// DefaultOrigin is the Origin a Client sends unless told otherwise. Test
// configs should list it in ZAZZZOR_ALLOWED_ORIGINS.
const DefaultOrigin = "https://zazzzor.test"

// Client talks to a handler over real HTTPS with a cookie jar, like a
// browser, and checks every response against api/openapi.yaml. A response
// the contract does not describe, including one for a path or method the
// contract does not know, fails the test.
type Client struct {
	// Origin is sent on every request unless overridden per request.
	Origin string

	t      testing.TB
	base   string
	client *http.Client
}

// Response is a fully read HTTP response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte

	t testing.TB
}

// JSON decodes the body into v, failing the test if it cannot.
func (r *Response) JSON(v any) {
	r.t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		r.t.Fatalf("decode response body %q: %v", r.Body, err)
	}
}

// RequestOption adjusts one request.
type RequestOption func(*http.Request)

// WithOrigin sends origin instead of Client.Origin.
func WithOrigin(origin string) RequestOption {
	return func(r *http.Request) { r.Header.Set("Origin", origin) }
}

// WithoutOrigin sends no Origin header.
func WithoutOrigin() RequestOption {
	return func(r *http.Request) { r.Header.Del("Origin") }
}

// WithHeader sets a request header.
func WithHeader(key, value string) RequestOption {
	return func(r *http.Request) { r.Header.Set(key, value) }
}

// NewClient serves handler on a test HTTPS server for the duration of t.
func NewClient(t testing.TB, handler http.Handler) *Client {
	t.Helper()
	if _, err := loadContract(); err != nil {
		t.Fatalf("testkit: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	// Clients hanging up mid-handshake are not test failures.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("testkit: cookie jar: %v", err)
	}
	hc := srv.Client()
	hc.Jar = jar
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{Origin: DefaultOrigin, t: t, base: srv.URL, client: hc}
}

// Do sends a request to path (e.g. "/api/v1/users/@me", query allowed).
// body may be nil (no body), []byte or string (sent as is) or any other
// value (sent as JSON); a non-nil body is labelled application/json unless
// an option sets Content-Type.
func (c *Client) Do(method, path string, body any, opts ...RequestOption) *Response {
	c.t.Helper()
	var raw []byte
	switch b := body.(type) {
	case nil:
	case []byte:
		raw = b
	case string:
		raw = []byte(b)
	default:
		var err error
		if raw, err = json.Marshal(b); err != nil {
			c.t.Fatalf("testkit: encode request body: %v", err)
		}
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(c.t.Context(), method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("testkit: build %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Origin != "" {
		req.Header.Set("Origin", c.Origin)
	}
	for _, opt := range opts {
		opt(req)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("testkit: %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("testkit: read %s %s: %v", method, path, err)
	}

	contract, err := loadContract()
	if err != nil {
		c.t.Fatalf("testkit: %v", err)
	}
	if err := contract.validate(req, resp.StatusCode, resp.Header, respBody); err != nil {
		c.t.Fatalf("testkit: %v\nresponse body: %s", err, truncate(respBody, 2048))
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: respBody, t: c.t}
}

func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return append(b[:n:n], "..."...)
}

// URL is the base URL of the test server (https://127.0.0.1:port).
func (c *Client) URL() string { return c.base }

// HTTPClient is the client with the cookie jar, e.g. for a WebSocket dial
// that must carry the session cookie.
func (c *Client) HTTPClient() *http.Client { return c.client }
