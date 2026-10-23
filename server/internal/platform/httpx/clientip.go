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

// ClientKey is the per-client rate-limit key of r: the IPv4 client address,
// or the /64 of an IPv6 one (one subscriber usually holds a whole /64, so
// per-address limits would be trivial to dodge). Zones are dropped and
// IPv4-mapped addresses count as IPv4.
func ClientKey(r *http.Request, trusted []netip.Prefix) string {
	a := ClientIP(r, trusted).WithZone("").Unmap()
	if !a.Is6() {
		return a.String()
	}
	return netip.PrefixFrom(a, 64).Masked().String()
}
