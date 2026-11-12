package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestSessionFailuresAreLimitedBeforeDatabase(t *testing.T) {
	now := time.Now()
	pool := testkit.Postgres(t)
	s := NewService(Options{Pool: pool, IDs: snowflake.NewGenerator(0, time.Now), Now: func() time.Time { return now }})
	_, valid, err := s.Register(t.Context(), RegisterInput{Username: "reader", Password: "password123"}, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	handler := Middleware(s)(Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	request := func(token, addr string, status int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(t.Context())
		r.RemoteAddr = addr
		r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("status = %d, want %d", w.Code, status)
		}
	}
	const addr = "192.0.2.10:10000"
	for range 35 {
		request(valid, addr, http.StatusNoContent)
	}
	fail := func() {
		t.Helper()
		token, _, err := newToken()
		if err != nil {
			t.Fatalf("newToken: %v", err)
		}
		request(token, addr, http.StatusUnauthorized)
	}
	before := pool.Stat().AcquireCount()
	for range 35 {
		fail()
	}
	if got := pool.Stat().AcquireCount() - before; got != 30 {
		t.Errorf("lookups after 35 failures = %d, want 30", got)
	}
	request(valid, "192.0.2.11:10000", http.StatusNoContent)
	now = now.Add(2 * time.Second)
	before = pool.Stat().AcquireCount()
	fail()
	if got := pool.Stat().AcquireCount() - before; got != 1 {
		t.Errorf("lookups after refill = %d, want 1", got)
	}
}
