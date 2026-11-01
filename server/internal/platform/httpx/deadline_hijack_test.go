package httpx_test

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

// pipeHijacker models a server that returns a connection with its deadlines.
type pipeHijacker struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (w *pipeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func TestWithoutDeadlinesClearsHijackedConnection(t *testing.T) {
	conn, peer := net.Pipe()
	t.Cleanup(func() { _ = conn.Close(); _ = peer.Close() })
	if err := conn.SetDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("expire inherited deadline: %v", err)
	}
	if err := peer.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("bound peer write: %v", err)
	}
	written := make(chan error, 1)
	go func() { _, err := peer.Write([]byte("x")); written <- err }()
	h := httpx.WithoutDeadlines(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		owned, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		var data [1]byte
		if _, err := io.ReadFull(owned, data[:]); err != nil {
			t.Fatalf("read hijacked connection after inherited deadline: %v", err)
		}
		if data[0] != 'x' {
			t.Fatalf("hijacked data = %q, want x", data)
		}
	}))
	h.ServeHTTP(&pipeHijacker{httptest.NewRecorder(), conn}, httptest.NewRequest(http.MethodGet, "/", nil))
	if err := <-written; err != nil {
		t.Fatalf("write hijacked peer: %v", err)
	}
}

// brokenDeadlineConn cannot take ownership without a stale deadline.
type brokenDeadlineConn struct {
	net.Conn
	err error
}

func (c brokenDeadlineConn) SetDeadline(time.Time) error { return c.err }

func TestWithoutDeadlinesClosesFailedHijack(t *testing.T) {
	conn, peer := net.Pipe()
	t.Cleanup(func() { _ = conn.Close(); _ = peer.Close() })
	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("bound peer read: %v", err)
	}
	failure := errors.New("deadline operation failed")
	h := httpx.WithoutDeadlines(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := http.NewResponseController(w).Hijack()
		if !errors.Is(err, failure) {
			t.Fatalf("hijack error = %v, want deadline failure", err)
		}
	}))
	h.ServeHTTP(&pipeHijacker{httptest.NewRecorder(), brokenDeadlineConn{conn, failure}}, httptest.NewRequest(http.MethodGet, "/", nil))
	var data [1]byte
	if _, err := peer.Read(data[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("failed hijack peer read = %v, want EOF", err)
	}
}

func TestWithoutDeadlinesPreservesUnsupportedHijack(t *testing.T) {
	h := httpx.WithoutDeadlines(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := http.NewResponseController(w).Hijack()
		if !errors.Is(err, http.ErrNotSupported) {
			t.Fatalf("hijack error = %v, want unsupported writer", err)
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
