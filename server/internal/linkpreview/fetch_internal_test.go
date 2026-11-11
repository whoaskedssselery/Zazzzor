package linkpreview

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestUnsafeTargets(t *testing.T) {
	f := New()
	f.connect = func(_ context.Context, _, address string) (net.Conn, error) {
		t.Errorf("unsafe dial attempted to %s", address)
		return nil, fmt.Errorf("unexpected dial")
	}
	f.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.1.1.1")}, nil
	}
	for _, target := range []string{"127.0.0.1", "10.0.0.1", "192.168.0.1", "169.254.169.254", "100.64.0.1", "[::1]", "[::ffff:127.0.0.1]", "[fc00::1]", "[64:ff9b::7f00:1]", "[2002:7f00:1::]", "[::127.0.0.1]", "0.0.0.1", "240.0.0.1", "intranet.test", "8.8.8.8:8080", "user@8.8.8.8"} {
		if _, err := f.read(context.Background(), "http://"+target); err == nil {
			t.Errorf("read(%q) succeeded, want refusal", target)
		}
	}
}

func TestFetchRedirectsAndLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/private":
			http.Redirect(w, r, "http://127.0.0.1", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/large":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, strings.Repeat("x", maxHTML+100))
		case "/slow":
			<-r.Context().Done()
		}
	}))
	defer srv.Close()
	f := New()
	f.connect = func(_ context.Context, _, address string) (net.Conn, error) {
		t.Errorf("unsafe dial attempted to %s", address)
		return nil, fmt.Errorf("unexpected dial")
	}
	f.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	f.connect = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "8.8.8.8:80" {
			return nil, fmt.Errorf("unexpected dial %s", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	f.client.Timeout = 300 * time.Millisecond
	for _, path := range []string{"/private", "/loop", "/slow"} {
		if _, err := f.read(context.Background(), "http://example.test"+path); err == nil {
			t.Errorf("read(%s) succeeded, want failure", path)
		}
	}
	body, err := f.read(context.Background(), "http://example.test/large")
	if err != nil || len(body.data) != maxHTML {
		t.Fatalf("read large length, error = %d, %v, want %d, nil", len(body.data), err, maxHTML)
	}
}
