package config_test

import (
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/config"
)

// testSecret is 32 characters, the shortest secret LiveKit accepts.
const testSecret = "0123456789abcdef0123456789abcdef"

// voiceEnv is a valid configuration plus the given variables.
func voiceEnv(extra map[string]string) func(string) string {
	m := map[string]string{
		"ZAZZZOR_DATABASE_URL":    "postgres://db",
		"ZAZZZOR_ALLOWED_ORIGINS": "https://a.example",
	}
	for k, v := range extra {
		m[k] = v
	}
	return envFrom(m)
}

func TestLoadLeavesVoiceOffWithoutCredentials(t *testing.T) {
	cfg, err := config.Load(voiceEnv(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.VoiceEnabled() {
		t.Error("VoiceEnabled() = true without credentials, want false")
	}
}

func TestLoadEnablesVoiceWithCredentials(t *testing.T) {
	cfg, err := config.Load(voiceEnv(map[string]string{
		"LIVEKIT_API_KEY":    "APIkey",
		"LIVEKIT_API_SECRET": testSecret,
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.VoiceEnabled() {
		t.Error("VoiceEnabled() = false with credentials, want true")
	}
	if cfg.LiveKitURL != config.DefaultLiveKitURL {
		t.Errorf("LiveKitURL = %q, want DefaultLiveKitURL %q", cfg.LiveKitURL, config.DefaultLiveKitURL)
	}
	if cfg.LiveKitAPIKey != "APIkey" || cfg.LiveKitAPISecret != testSecret {
		t.Error("the LiveKit credentials were not read as given")
	}
}

func TestLoadRejectsHalfOfTheCredentials(t *testing.T) {
	for missing, set := range map[string]map[string]string{
		"LIVEKIT_API_SECRET": {"LIVEKIT_API_KEY": "APIkey"},
		"LIVEKIT_API_KEY":    {"LIVEKIT_API_SECRET": testSecret},
	} {
		_, err := config.Load(voiceEnv(set))
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("Load without %s: err = %v, want one naming it", missing, err)
		}
	}
}

func TestLoadRejectsShortSecret(t *testing.T) {
	_, err := config.Load(voiceEnv(map[string]string{
		"LIVEKIT_API_KEY":    "APIkey",
		"LIVEKIT_API_SECRET": testSecret[:31],
	}))
	if err == nil || !strings.Contains(err.Error(), "LIVEKIT_API_SECRET") {
		t.Fatalf("Load with a 31-character secret: err = %v, want one naming LIVEKIT_API_SECRET", err)
	}
	if strings.Contains(err.Error(), testSecret[:31]) {
		t.Error("the error shows the secret")
	}
}

func TestLoadValidatesLiveKitURL(t *testing.T) {
	for _, u := range []string{"ws://lk:7880", "http://", "http://user:pw@lk:7880", "http://lk:7880/x",
		"http://lk:7880?a=1", "http://lk:7880#f", "lk:7880"} {
		_, err := config.Load(voiceEnv(map[string]string{
			"LIVEKIT_API_KEY":     "APIkey",
			"LIVEKIT_API_SECRET":  testSecret,
			"ZAZZZOR_LIVEKIT_URL": u,
		}))
		if err == nil || !strings.Contains(err.Error(), "ZAZZZOR_LIVEKIT_URL") {
			t.Errorf("Load with ZAZZZOR_LIVEKIT_URL=%q: err = %v, want one naming the variable", u, err)
		}
	}
	for _, u := range []string{"http://172.31.250.1:7880", "https://lk.example/"} {
		cfg, err := config.Load(voiceEnv(map[string]string{
			"LIVEKIT_API_KEY":     "APIkey",
			"LIVEKIT_API_SECRET":  testSecret,
			"ZAZZZOR_LIVEKIT_URL": u,
		}))
		if err != nil {
			t.Errorf("Load with ZAZZZOR_LIVEKIT_URL=%q: %v", u, err)
			continue
		}
		if cfg.LiveKitURL != u {
			t.Errorf("LiveKitURL = %q, want %q", cfg.LiveKitURL, u)
		}
	}
}
