package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"
)

const (
	// CookieName is the session cookie.
	CookieName = "zz_session"
	// SessionTTL is how long a session lives after its last renewal.
	SessionTTL = 30 * 24 * time.Hour
	// renewEvery is the minimum gap between two renewals of one session.
	renewEvery = 24 * time.Hour

	tokenLen = 32
	// maxUserAgent bounds the stored User-Agent (bytes).
	maxUserAgent = 512
)

// newToken returns a random session token (base64url, unpadded) and the
// SHA-256 that is stored instead of it.
func newToken() (token string, hash []byte, err error) {
	raw := make([]byte, tokenLen)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("auth: token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

// hashToken is the stored form of a token: SHA-256 of the cookie value.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// wellFormedToken rejects values that cannot be a token before they reach
// the database.
func wellFormedToken(token string) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(tokenLen) {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil
}

func (s *Service) setSessionCookie(w http.ResponseWriter, token string) {
	//nolint:gosec // G124: Secure follows ZAZZZOR_COOKIE_SECURE (default true); HttpOnly and SameSite=Lax are set.
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) clearSessionCookie(w http.ResponseWriter) {
	//nolint:gosec // G124: Secure follows ZAZZZOR_COOKIE_SECURE (default true); HttpOnly and SameSite=Lax are set.
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// truncateUTF8 cuts s to at most n bytes without splitting a rune.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
