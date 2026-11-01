package httpx_test

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

type readResult struct {
	err     error
	elapsed time.Duration
}

// slowBodyServer serves a handler that reads the whole body and reports how
// the read ended, then answers 200.
func slowBodyServer(t *testing.T, opts httpx.DeadlineOptions) (*httptest.Server, <-chan readResult) {
	t.Helper()
	results := make(chan readResult, 1)
	h := httpx.Deadlines(opts)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		_, err := io.Copy(io.Discard, r.Body)
		results <- readResult{err: err, elapsed: time.Since(start)}
		if err != nil {
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewUnstartedServer(h)
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, results
}

// sendStalled writes the request head and a few body bytes, stalls for
// stall, then writes the rest of the body.
func sendStalled(t *testing.T, addr string, extraHeaders string, stall time.Duration) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	body := strings.Repeat("x", 64)
	head := "GET /x HTTP/1.1\r\nHost: test\r\nContent-Length: 64\r\n" + extraHeaders + "\r\n"
	if _, err := io.WriteString(conn, head+body[:4]); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(stall)
		_, _ = io.WriteString(conn, body[4:])
	}()
	return conn
}

func TestDeadlinesCutOffSlowBody(t *testing.T) {
	const readDeadline = 200 * time.Millisecond
	srv, results := slowBodyServer(t, httpx.DeadlineOptions{Read: readDeadline, Write: time.Second})
	sendStalled(t, srv.Listener.Addr().String(), "", 5*time.Second)

	select {
	case res := <-results:
		if res.err == nil {
			t.Fatal("body read succeeded, want deadline error")
		}
		if res.elapsed > readDeadline+time.Second {
			t.Errorf("read ended after %v, want about %v", res.elapsed, readDeadline)
		}
	case <-time.After(readDeadline + 2*time.Second):
		t.Fatal("handler still reading the body long after the read deadline")
	}
}

// An Upgrade header does not buy a request out of the deadlines.
func TestDeadlinesApplyToUpgradeRequests(t *testing.T) {
	const deadline = 200 * time.Millisecond
	srv, results := slowBodyServer(t, httpx.DeadlineOptions{Read: deadline, Write: time.Second})
	sendStalled(t, srv.Listener.Addr().String(), "Connection: Upgrade\r\nUpgrade: websocket\r\n", 5*time.Second)

	select {
	case res := <-results:
		if res.err == nil {
			t.Fatal("upgrade-looking request escaped the read deadline")
		}
	case <-time.After(deadline + 2*time.Second):
		t.Fatal("handler still reading the body long after the read deadline")
	}
}

func TestWithoutDeadlinesKeepsOrdinaryHTTPBounded(t *testing.T) {
	const deadline = 100 * time.Millisecond
	results := make(chan readResult, 1)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		_, err := io.Copy(io.Discard, r.Body)
		results <- readResult{err: err, elapsed: time.Since(start)}
		w.WriteHeader(http.StatusOK)
	})
	h := httpx.Deadlines(httpx.DeadlineOptions{Read: deadline, Write: deadline})(httpx.WithoutDeadlines(inner))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	sendStalled(t, srv.Listener.Addr().String(), "", 4*deadline)

	select {
	case res := <-results:
		if res.err == nil {
			t.Fatal("ordinary HTTP body escaped the deadline without a hijack")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not finish")
	}
}

func TestWithoutDeadlinesToleratesPlainWriters(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WithoutDeadlines(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestDeadlineOptionsDefaults(t *testing.T) {
	if httpx.DefaultReadDeadline != 15*time.Second || httpx.DefaultWriteDeadline != 30*time.Second {
		t.Errorf("defaults = %v/%v, want 15s/30s", httpx.DefaultReadDeadline, httpx.DefaultWriteDeadline)
	}
	// Works on writers without deadline support (e.g. ResponseRecorder).
	rec := httptest.NewRecorder()
	httpx.Deadlines(httpx.DeadlineOptions{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d", rec.Code)
	}
}
