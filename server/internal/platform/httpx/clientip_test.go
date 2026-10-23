package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

func TestClientIPTrustsOnlyProxies(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("172.18.0.0/16"),
	}
	cases := []struct {
		name, remote string
		xff          []string
		want         string
	}{
		{"trusted proxy", "127.0.0.1:5000", []string{"1.2.3.4"}, "1.2.3.4"},
		{"untrusted peer ignores header", "5.6.7.8:5000", []string{"1.2.3.4"}, "5.6.7.8"},
		{"rightmost untrusted wins", "127.0.0.1:5000", []string{"9.9.9.9, 1.2.3.4, 172.18.0.5"}, "1.2.3.4"},
		{"multiple header lines", "127.0.0.1:5000", []string{"9.9.9.9", "1.2.3.4"}, "1.2.3.4"},
		{"ipv6 proxy", "[::1]:5000", []string{"2001:db8::1"}, "2001:db8::1"},
		{"no header", "127.0.0.1:5000", nil, "127.0.0.1"},
		{"garbage entry stops walk", "127.0.0.1:5000", []string{"1.2.3.4, garbage"}, "127.0.0.1"},
		{"all trusted falls back to leftmost", "127.0.0.1:5000", []string{"172.18.0.9, 172.18.0.5"}, "172.18.0.9"},
		{"ipv4-mapped peer", "[::ffff:127.0.0.1]:5000", []string{"1.2.3.4"}, "1.2.3.4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			got := httpx.ClientIP(r, trusted)
			if got != netip.MustParseAddr(tc.want) {
				t.Errorf("ClientIP = %v, want %s", got, tc.want)
			}
		})
	}
}
