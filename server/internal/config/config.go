// Package config reads and validates the server configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config is the validated server configuration.
type Config struct {
	HTTPAddr       string         // ZAZZZOR_HTTP_ADDR, default ":8080"
	MetricsAddr    string         // ZAZZZOR_METRICS_ADDR, default ":9090"
	DatabaseURL    string         // ZAZZZOR_DATABASE_URL, required
	AllowedOrigins []string       // ZAZZZOR_ALLOWED_ORIGINS, comma-separated, required
	TrustedProxies []netip.Prefix // ZAZZZOR_TRUSTED_PROXIES, default "127.0.0.1/32,::1/128"
	AdminUsernames []string       // ZAZZZOR_ADMIN_USERNAMES, comma-separated, lower-cased
	DataDir        string         // ZAZZZOR_DATA_DIR, default "/data"
	CookieSecure   bool           // ZAZZZOR_COOKIE_SECURE, default true
	FileRetention  time.Duration  // ZAZZZOR_FILE_RETENTION, default DefaultFileRetention; over a day
}

// Defaults for optional variables.
const (
	DefaultHTTPAddr       = ":8080"
	DefaultMetricsAddr    = ":9090"
	DefaultTrustedProxies = "127.0.0.1/32,::1/128"
	DefaultDataDir        = "/data"
	// DefaultFileRetention keeps deleted images through eight daily
	// backups.
	DefaultFileRetention = 8 * 24 * time.Hour
)

// backupInterval is how often deploy/scripts/backup-job.sh backs up.
const backupInterval = 24 * time.Hour

// Load builds a Config from env (usually os.Getenv). The returned error names
// every missing or invalid variable at once, never their values.
func Load(env func(string) string) (Config, error) {
	var errs []error
	get := func(name, def string) string {
		if v := strings.TrimSpace(env(name)); v != "" {
			return v
		}
		return def
	}

	cfg := Config{
		HTTPAddr:    get("ZAZZZOR_HTTP_ADDR", DefaultHTTPAddr),
		MetricsAddr: get("ZAZZZOR_METRICS_ADDR", DefaultMetricsAddr),
		DatabaseURL: get("ZAZZZOR_DATABASE_URL", ""),
		DataDir:     get("ZAZZZOR_DATA_DIR", DefaultDataDir),
	}

	for _, a := range []struct{ name, value string }{
		{"ZAZZZOR_HTTP_ADDR", cfg.HTTPAddr},
		{"ZAZZZOR_METRICS_ADDR", cfg.MetricsAddr},
	} {
		if _, _, err := net.SplitHostPort(a.value); err != nil {
			errs = append(errs, fmt.Errorf("%s: must be host:port", a.name))
		}
	}

	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("ZAZZZOR_DATABASE_URL: required"))
	}

	cfg.AllowedOrigins = splitList(env("ZAZZZOR_ALLOWED_ORIGINS"))
	if len(cfg.AllowedOrigins) == 0 {
		errs = append(errs, errors.New("ZAZZZOR_ALLOWED_ORIGINS: required"))
	}
	for _, o := range cfg.AllowedOrigins {
		if !validOrigin(o) {
			errs = append(errs, fmt.Errorf("ZAZZZOR_ALLOWED_ORIGINS: %q is not an origin like https://host[:port]", o))
		}
	}

	for _, p := range splitList(get("ZAZZZOR_TRUSTED_PROXIES", DefaultTrustedProxies)) {
		prefix, err := parsePrefix(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("ZAZZZOR_TRUSTED_PROXIES: %q is not an IP or CIDR", p))
			continue
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, prefix)
	}

	for _, u := range splitList(env("ZAZZZOR_ADMIN_USERNAMES")) {
		cfg.AdminUsernames = append(cfg.AdminUsernames, strings.ToLower(u))
	}

	secure, err := strconv.ParseBool(get("ZAZZZOR_COOKIE_SECURE", "true"))
	if err != nil {
		errs = append(errs, errors.New("ZAZZZOR_COOKIE_SECURE: must be true or false"))
	}
	cfg.CookieSecure = secure

	// Deleted images must outlive the next daily backup, or a restored
	// database can refer to an image that no archive holds.
	cfg.FileRetention = DefaultFileRetention
	if v := get("ZAZZZOR_FILE_RETENTION", ""); v != "" {
		retention, err := time.ParseDuration(v)
		if err != nil || retention <= backupInterval {
			errs = append(errs, fmt.Errorf("ZAZZZOR_FILE_RETENTION: must be a duration over %s, like 192h", backupInterval))
		}
		cfg.FileRetention = retention
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, err
		}
		return p.Masked(), nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// validOrigin accepts exactly what browsers send in the Origin header:
// scheme://host[:port] with no path, query, fragment or credentials.
func validOrigin(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
		u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" &&
		s == u.Scheme+"://"+u.Host
}
