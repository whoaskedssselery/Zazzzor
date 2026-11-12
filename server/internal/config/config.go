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
	HTTPAddr             string         // ZAZZZOR_HTTP_ADDR, default ":8080"
	MetricsAddr          string         // ZAZZZOR_METRICS_ADDR, default ":9090"
	DatabaseURL          string         // ZAZZZOR_DATABASE_URL, required
	AllowedOrigins       []string       // ZAZZZOR_ALLOWED_ORIGINS, comma-separated, required
	TrustedProxies       []netip.Prefix // ZAZZZOR_TRUSTED_PROXIES, default "127.0.0.1/32,::1/128"
	AdminUsernames       []string       // ZAZZZOR_ADMIN_USERNAMES, comma-separated, lower-cased
	DataDir              string         // ZAZZZOR_DATA_DIR, default "/data"
	CookieSecure         bool           // ZAZZZOR_COOKIE_SECURE, default true
	AttachmentQuotaBytes int64          // ZAZZZOR_ATTACHMENT_QUOTA_BYTES; zero disables the quota
	MinFreeBytes         uint64         // ZAZZZOR_MIN_FREE_BYTES; zero disables the floor
	FileRetention        time.Duration  // ZAZZZOR_FILE_RETENTION, default DefaultFileRetention; over a day
	// LiveKitURL is where the API calls the LiveKit server API
	// (ZAZZZOR_LIVEKIT_URL, default DefaultLiveKitURL).
	LiveKitURL string
	// LiveKitAPIKey and LiveKitAPISecret (LIVEKIT_API_KEY,
	// LIVEKIT_API_SECRET) sign tokens and verify webhooks. Both empty turns
	// voice off; one without the other is an error.
	LiveKitAPIKey    string
	LiveKitAPISecret string
}

// VoiceEnabled reports whether the LiveKit credentials are set.
func (c Config) VoiceEnabled() bool { return c.LiveKitAPIKey != "" }

// Defaults for optional variables.
const (
	DefaultHTTPAddr       = ":8080"
	DefaultMetricsAddr    = ":9090"
	DefaultTrustedProxies = "127.0.0.1/32,::1/128"
	DefaultDataDir        = "/data"
	// DefaultFileRetention keeps deleted images through eight daily
	// backups.
	DefaultFileRetention = 8 * 24 * time.Hour
	// DefaultAttachmentQuotaBytes is the combined size of one user's uploads.
	DefaultAttachmentQuotaBytes = 2 << 30
	// DefaultMinFreeBytes reserves space on the files volume.
	DefaultMinFreeBytes = 2 << 30
	// DefaultLiveKitURL is LiveKit on the same host network.
	DefaultLiveKitURL = "http://127.0.0.1:7880"
)

// minLiveKitSecret is the shortest API secret LiveKit itself accepts.
const minLiveKitSecret = 32

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
	quota, err := strconv.ParseInt(get("ZAZZZOR_ATTACHMENT_QUOTA_BYTES", strconv.FormatInt(DefaultAttachmentQuotaBytes, 10)), 10, 64)
	if err != nil || quota < 0 {
		errs = append(errs, errors.New("ZAZZZOR_ATTACHMENT_QUOTA_BYTES: must be a non-negative byte count"))
	}
	cfg.AttachmentQuotaBytes = quota
	floor, err := strconv.ParseUint(get("ZAZZZOR_MIN_FREE_BYTES", strconv.FormatUint(DefaultMinFreeBytes, 10)), 10, 64)
	if err != nil {
		errs = append(errs, errors.New("ZAZZZOR_MIN_FREE_BYTES: must be a non-negative byte count"))
	}
	cfg.MinFreeBytes = floor
	if v := get("ZAZZZOR_FILE_RETENTION", ""); v != "" {
		retention, err := time.ParseDuration(v)
		if err != nil || retention <= backupInterval {
			errs = append(errs, fmt.Errorf("ZAZZZOR_FILE_RETENTION: must be a duration over %s, like 192h", backupInterval))
		}
		cfg.FileRetention = retention
	}

	errs = append(errs, loadLiveKit(&cfg, get)...)

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// loadLiveKit reads the LiveKit settings into cfg. Without both
// credentials voice is off and the URL is not checked.
func loadLiveKit(cfg *Config, get func(name, def string) string) []error {
	cfg.LiveKitURL = get("ZAZZZOR_LIVEKIT_URL", DefaultLiveKitURL)
	cfg.LiveKitAPIKey = get("LIVEKIT_API_KEY", "")
	cfg.LiveKitAPISecret = get("LIVEKIT_API_SECRET", "")
	var errs []error
	switch {
	case cfg.LiveKitAPIKey == "" && cfg.LiveKitAPISecret == "":
		return nil
	case cfg.LiveKitAPIKey == "":
		errs = append(errs, errors.New("LIVEKIT_API_KEY: required when LIVEKIT_API_SECRET is set"))
	case cfg.LiveKitAPISecret == "":
		errs = append(errs, errors.New("LIVEKIT_API_SECRET: required when LIVEKIT_API_KEY is set"))
	case len(cfg.LiveKitAPISecret) < minLiveKitSecret:
		errs = append(errs, fmt.Errorf("LIVEKIT_API_SECRET: must be at least %d characters", minLiveKitSecret))
	}
	if !validServiceURL(cfg.LiveKitURL) {
		errs = append(errs, errors.New("ZAZZZOR_LIVEKIT_URL: must be a URL like http://host:port"))
	}
	return errs
}

// validServiceURL accepts http(s)://host[:port] with at most a "/" path and
// no credentials, query or fragment.
func validServiceURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil &&
		(u.Path == "" || u.Path == "/") && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
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
