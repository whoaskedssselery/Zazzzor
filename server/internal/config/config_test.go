package config_test

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/config"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadRequiresDatabaseAndOrigins(t *testing.T) {
	_, err := config.Load(envFrom(nil))
	if err == nil {
		t.Fatal("Load with empty env: want error, got nil")
	}
	for _, name := range []string{"ZAZZZOR_DATABASE_URL", "ZAZZZOR_ALLOWED_ORIGINS"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err, name)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(envFrom(map[string]string{
		"ZAZZZOR_DATABASE_URL":    "postgres://db/zazzzor",
		"ZAZZZOR_ALLOWED_ORIGINS": "https://a.example",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.MetricsAddr != ":9090" {
		t.Errorf("MetricsAddr = %q, want :9090", cfg.MetricsAddr)
	}
	if cfg.DataDir != "/data" {
		t.Errorf("DataDir = %q, want /data", cfg.DataDir)
	}
	if !cfg.CookieSecure {
		t.Error("CookieSecure = false, want true")
	}
	want := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}
	if !slices.Equal(cfg.TrustedProxies, want) {
		t.Errorf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	if !slices.Equal(cfg.AllowedOrigins, []string{"https://a.example"}) {
		t.Errorf("AllowedOrigins = %v", cfg.AllowedOrigins)
	}
	if len(cfg.AdminUsernames) != 0 {
		t.Errorf("AdminUsernames = %v, want empty", cfg.AdminUsernames)
	}
}

func TestLoadParsesListsAndFlags(t *testing.T) {
	cfg, err := config.Load(envFrom(map[string]string{
		"ZAZZZOR_DATABASE_URL":    "postgres://db",
		"ZAZZZOR_ALLOWED_ORIGINS": " https://a.example , https://b.example ,",
		"ZAZZZOR_TRUSTED_PROXIES": "10.0.0.0/8, 172.16.0.1",
		"ZAZZZOR_ADMIN_USERNAMES": "Alice,bob",
		"ZAZZZOR_COOKIE_SECURE":   "false",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(cfg.AllowedOrigins, []string{"https://a.example", "https://b.example"}) {
		t.Errorf("AllowedOrigins = %v", cfg.AllowedOrigins)
	}
	want := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.1/32")}
	if !slices.Equal(cfg.TrustedProxies, want) {
		t.Errorf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	if !slices.Equal(cfg.AdminUsernames, []string{"alice", "bob"}) {
		t.Errorf("AdminUsernames = %v", cfg.AdminUsernames)
	}
	if cfg.CookieSecure {
		t.Error("CookieSecure = true, want false")
	}
}

func TestLoadReportsAllInvalidValues(t *testing.T) {
	_, err := config.Load(envFrom(map[string]string{
		"ZAZZZOR_DATABASE_URL":    "postgres://db",
		"ZAZZZOR_ALLOWED_ORIGINS": "https://a.example",
		"ZAZZZOR_TRUSTED_PROXIES": "not-a-cidr",
		"ZAZZZOR_COOKIE_SECURE":   "maybe",
	}))
	if err == nil {
		t.Fatal("want error")
	}
	for _, name := range []string{"ZAZZZOR_TRUSTED_PROXIES", "ZAZZZOR_COOKIE_SECURE"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err, name)
		}
	}
}
