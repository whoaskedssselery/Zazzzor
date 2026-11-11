// Package linkpreview fetches public HTTP metadata for message embeds.
package linkpreview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/idna"
)

const (
	// MaxURL bounds both requested URLs and cache keys in bytes.
	MaxURL    = 2048
	maxHTML   = 512 << 10
	maxImage  = 5 << 20
	maxJSON   = 64 << 10
	timeout   = 5 * time.Second
	userAgent = "Mozilla/5.0 (compatible; ZazzzorBot/1.0; +link-preview)"
)

var (
	errTarget      = errors.New("linkpreview: unsafe HTTP target")
	errContentType = errors.New("linkpreview: unsupported content type")
	errTooLarge    = errors.New("linkpreview: image exceeds size limit")
)

// statusError is an answer other than 200.
type statusError int

func (e statusError) Error() string { return fmt.Sprintf("linkpreview: HTTP status %d", int(e)) }
var globalIPv6 = netip.MustParsePrefix("2000::/3")
var specialNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/32"), // Teredo
	netip.MustParsePrefix("2002::/16"), // 6to4
}

// Fetcher fetches public URLs without ambient cookies, credentials or proxy settings.
type Fetcher struct {
	client  *http.Client
	lookup  func(context.Context, string, string) ([]netip.Addr, error)
	connect func(context.Context, string, string) (net.Conn, error)
	mu      sync.Mutex
	hosts   map[string]*hostLimit
	// budget bounds each stage of a preview (the page, its picture, oEmbed) on its own.
	budget time.Duration
}

type hostLimit struct {
	slots chan struct{}
	users int
}

// New constructs a fetcher with a transport that only dials validated IPs.
func New() *Fetcher {
	f := &Fetcher{lookup: net.DefaultResolver.LookupNetIP, connect: (&net.Dialer{}).DialContext,
		hosts: make(map[string]*hostLimit), budget: timeout}
	transport := &http.Transport{DialContext: f.dial, DisableKeepAlives: true, MaxResponseHeaderBytes: 64 << 10}
	f.client = &http.Client{Transport: limitedTransport{f, transport}, Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errors.New("linkpreview: too many redirects")
			}
			req.Header.Del("Referer")
			return validateURL(req.URL)
		}}
	return f
}

// validateURL rejects unsupported request forms before starting any network work.
func validateURL(u *url.URL) error {
	port := u.Port()
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" ||
		(port != "" && port != "80" && port != "443") || len(u.String()) > MaxURL {
		return errTarget
	}
	return nil
}

// dial resolves and checks all addresses, then connects to an address literal.
func (f *Fetcher) dial(ctx context.Context, network, address string) (net.Conn, error) {
	// Transport may keep dialing after its request is canceled.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("linkpreview: split address: %w", err)
	}
	literal, err := netip.ParseAddr(host)
	ips := []netip.Addr{literal}
	if err != nil {
		ips, err = f.lookup(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("linkpreview: resolve address: %w", err)
		}
	}
	if len(ips) == 0 {
		return nil, errTarget
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errTarget
		}
	}
	for _, ip := range ips {
		var conn net.Conn
		conn, err = f.connect(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("linkpreview: dial address: %w", err)
}

// publicIP excludes local and special-purpose ranges, including IPv6
// translation and tunneling prefixes that can reach IPv4 destinations.
func publicIP(ip netip.Addr) bool {
	if ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !globalIPv6.Contains(ip)) {
		return false
	}
	for _, network := range specialNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

// canonicalHost groups equivalent literal and DNS spellings into one limit.
func canonicalHost(host string) string {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().String()
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		return strings.TrimSuffix(ascii, ".")
	}
	return host
}

type limitedTransport struct {
	fetcher   *Fetcher
	transport *http.Transport
}

// RoundTrip holds a host slot until the response body is closed.
func (l limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := canonicalHost(req.URL.Hostname())
	release, err := l.fetcher.acquire(req.Context(), host)
	if err != nil {
		return nil, err
	}
	res, err := l.transport.RoundTrip(req)
	if err != nil {
		release()
		return nil, fmt.Errorf("linkpreview: send request: %w", err)
	}
	res.Body = &hostBody{ReadCloser: res.Body, release: sync.OnceFunc(release)}
	return res, nil
}

type hostBody struct {
	io.ReadCloser
	release func()
}

func (b *hostBody) Close() error {
	defer b.release()
	return b.ReadCloser.Close()
}

// acquire bounds concurrent requests including redirects for each host.
func (f *Fetcher) acquire(ctx context.Context, host string) (func(), error) {
	f.mu.Lock()
	limit := f.hosts[host]
	if limit == nil {
		limit = &hostLimit{slots: make(chan struct{}, 2)}
		f.hosts[host] = limit
	}
	limit.users++
	f.mu.Unlock()
	forget := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		limit.users--
		if limit.users == 0 {
			delete(f.hosts, host)
		}
	}
	select {
	case limit.slots <- struct{}{}:
		return func() { <-limit.slots; forget() }, nil
	case <-ctx.Done():
		forget()
		return nil, fmt.Errorf("linkpreview: wait for host: %w", ctx.Err())
	}
}

type response struct {
	data        []byte
	contentType string
	mediaType   string
	url         *url.URL
}

// read obtains a bounded HTML prefix or a complete supported image.
func (f *Fetcher) read(ctx context.Context, raw string) (response, error) {
	return f.readKind(ctx, raw, false)
}

// readKind is read that also accepts a small JSON document when allowJSON
// is set: oEmbed answers, never an ordinary link.
func (f *Fetcher) readKind(ctx context.Context, raw string, allowJSON bool) (response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return response{}, fmt.Errorf("linkpreview: build request: %w", err)
	}
	if err = validateURL(req.URL); err != nil {
		return response{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := f.client.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("linkpreview: fetch resource: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return response{}, statusError(res.StatusCode)
	}
	contentType := res.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return response{}, fmt.Errorf("linkpreview: parse content type: %w", err)
	}
	var limit int64
	switch mediaType {
	case "text/html":
		limit = maxHTML
	case "application/json":
		if !allowJSON {
			return response{}, fmt.Errorf("%w %q", errContentType, mediaType)
		}
		limit = maxJSON
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		limit = maxImage + 1
	default:
		return response{}, fmt.Errorf("%w %q", errContentType, mediaType)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return response{}, fmt.Errorf("linkpreview: read body: %w", err)
	}
	if len(data) > maxImage {
		return response{}, errTooLarge
	}
	return response{data: data, contentType: contentType, mediaType: mediaType, url: res.Request.URL}, nil
}

// Host is the host of raw for logs: no path, query or credentials, which may carry tokens.
func Host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "invalid"
	}
	return canonicalHost(u.Hostname())
}

// reason names why a request failed, without the URL that url.Error carries.
func reason(err error) string {
	var status statusError
	switch {
	case errors.Is(err, errTarget):
		return "refused address"
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return "timeout"
	case errors.As(err, &status):
		return fmt.Sprintf("HTTP %d", int(status))
	case errors.Is(err, errContentType):
		return "content type"
	case errors.Is(err, errTooLarge):
		return "too large"
	default:
		return "network"
	}
}

// isTimeout reports a client or dial timeout.
func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}
