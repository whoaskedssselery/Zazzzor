package httpx

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP returns the address of the client that made r. X-Forwarded-For is
// consulted only when the direct peer is in trusted; then the rightmost entry
// that is not itself a trusted proxy wins, so a client cannot spoof its address
// by prepending entries. An unparseable entry stops the walk at the last
// address known to be good.
func ClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer := remoteAddr(r)
	if !peer.IsValid() || !isTrusted(peer, trusted) {
		return peer
	}

	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for h := range strings.SplitSeq(v, ",") {
			hops = append(hops, strings.TrimSpace(h))
		}
	}

	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(hops[i])
		if err != nil {
			return client
		}
		client = addr.Unmap()
		if !isTrusted(client, trusted) {
			return client
		}
	}
	return client
}

func remoteAddr(r *http.Request) netip.Addr {
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return ap.Addr().Unmap()
	}
	if a, err := netip.ParseAddr(r.RemoteAddr); err == nil {
		return a.Unmap()
	}
	return netip.Addr{}
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
