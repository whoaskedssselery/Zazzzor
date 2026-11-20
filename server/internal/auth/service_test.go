package auth_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

var _ auth.Accounts = (*auth.Service)(nil)

func userID(t *testing.T, u userBody) snowflake.ID {
	t.Helper()
	id, err := snowflake.Parse(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAccountsProfileAndAvatar(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.client()
	id := userID(t, register(t, c, "nina", "password123"))
	ctx := t.Context()

	u, err := e.svc.UpdateProfile(ctx, id, "  Нина  ")
	if err != nil || u.DisplayName != "Нина" || u.ID != id || u.Username != "nina" {
		t.Fatalf("UpdateProfile = %+v, %v", u, err)
	}
	var verr *auth.ValidationError
	if _, err = e.svc.UpdateProfile(ctx, id, " "); !errors.As(err, &verr) || verr.Fields[0].Field != "display_name" {
		t.Fatalf("empty display name: err = %v", err)
	}
	// Plain marks (LRM/RLM), combining accents and emoji ZWJ sequences are fine.
	for _, dn := range []string{"שלום" + cps(0x200F), "e" + cps(0x0301), cps(0x1F469, 0x200D, 0x1F4BB), cps(0x3164) + "a"} {
		if _, err = e.svc.UpdateProfile(ctx, id, dn); err != nil {
			t.Errorf("UpdateProfile(%q) = %v, want accepted", dn, err)
		}
	}
	for _, dn := range []string{cps(0x202E) + "evil", cps(0x3164, 0x3164), cps(0x200F)} {
		if _, err = e.svc.UpdateProfile(ctx, id, dn); !errors.As(err, &verr) {
			t.Errorf("UpdateProfile(%q) = %v, want ValidationError", dn, err)
		}
	}
	if _, err = e.svc.UpdateProfile(ctx, id, "  Нина  "); err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.UpdateProfile(ctx, 12345, "x"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("unknown user: err = %v", err)
	}

	hash := "0123456789abcdef"
	u, prev, err := e.svc.SetAvatar(ctx, id, &hash)
	if err != nil || u.AvatarHash == nil || *u.AvatarHash != hash || prev != nil {
		t.Fatalf("SetAvatar = %+v, previous %v, %v", u, prev, err)
	}
	if got := u.Public().AvatarURL; got == nil || *got != "/api/v1/files/avatars/"+id.String()+"/"+hash+".png" {
		t.Fatalf("avatar_url = %v", got)
	}
	bad := "../../etc/passwd"
	if _, _, err = e.svc.SetAvatar(ctx, id, &bad); err == nil {
		t.Fatal("SetAvatar accepted a hash that is not 16 hex digits")
	}
	if u, prev, err = e.svc.SetAvatar(ctx, id, nil); err != nil || u.AvatarHash != nil || u.Public().AvatarURL != nil ||
		prev == nil || *prev != hash {
		t.Fatalf("clear avatar = %+v, previous %v, %v", u, prev, err)
	}

	got, err := e.svc.GetUser(ctx, id)
	if err != nil || got.DisplayName != "Нина" {
		t.Fatalf("GetUser = %+v, %v", got, err)
	}
	if _, err := e.svc.GetUser(ctx, 999); !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("GetUser(unknown) err = %v", err)
	}
}

func TestUsersByIDs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.client()
	a := userID(t, register(t, c, "oleg", "password123"))
	boss, err := e.svc.CreateAdmin(t.Context(), "boss", "password123")
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	b := boss.ID
	us, err := e.svc.UsersByIDs(t.Context(), []snowflake.ID{b, 42, a, a})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]snowflake.ID, 0, len(us))
	for _, u := range us {
		ids = append(ids, u.ID)
	}
	if !slices.Equal(ids, []snowflake.ID{a, b}) {
		t.Fatalf("ids = %v, want [%v %v] (unknown and duplicate dropped, sorted)", ids, a, b)
	}
	if us[0].IsAdmin || !us[1].IsAdmin {
		t.Fatalf("IsAdmin = %v/%v, want only boss", us[0].IsAdmin, us[1].IsAdmin)
	}
	if us, err := e.svc.UsersByIDs(t.Context(), nil); err != nil || len(us) != 0 {
		t.Fatalf("UsersByIDs(nil) = %v, %v", us, err)
	}
}

func TestIsAdminCaseInsensitive(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *auth.Options) { o.AdminUsernames = []string{"Root_User"} })
	c := e.client()
	created, err := e.svc.CreateAdmin(t.Context(), "ROOT_user", "password123")
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	u, err := e.svc.GetUser(t.Context(), created.ID)
	if err != nil || !u.IsAdmin {
		t.Fatalf("GetUser = %+v, %v; want admin", u, err)
	}
	// The context user carries it too.
	tok := sessionCookie(t, c.Do(http.MethodPost, "/api/v1/auth/login", creds("root_user", "password123"))).Value
	cu, _, err := e.svc.Authenticate(t.Context(), tok)
	if err != nil || !cu.IsAdmin || cu.Username != "root_user" {
		t.Fatalf("Authenticate = %+v, %v", cu, err)
	}
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c1, c2 := e.client(), e.client()
	register(t, c1, "pavel", "password123")
	tok1 := sessionCookie(t, c1.Do(http.MethodPost, "/api/v1/auth/login", creds("pavel", "password123"))).Value
	c2.Do(http.MethodPost, "/api/v1/auth/login", creds("pavel", "password123"))
	u, keep, err := e.svc.Authenticate(t.Context(), tok1)
	if err != nil {
		t.Fatal(err)
	}
	before := len(e.pub.all())

	if err := e.svc.ChangePassword(t.Context(), u.ID, "wrong-current", "newpassword1", keep); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong current password: err = %v", err)
	}
	var verr *auth.ValidationError
	if err := e.svc.ChangePassword(t.Context(), u.ID, "password123", "short", keep); !errors.As(err, &verr) || verr.Fields[0].Field != "new_password" {
		t.Fatalf("short new password: err = %v", err)
	}
	if len(e.pub.all()) != before {
		t.Fatal("a failed change published events")
	}

	if err := e.svc.ChangePassword(t.Context(), u.ID, "password123", "newpassword1", keep); err != nil {
		t.Fatal(err)
	}
	if resp := c1.Do(http.MethodGet, "/api/v1/users/@me", nil); resp.Status != http.StatusOK {
		t.Fatalf("kept session: @me = %d", resp.Status)
	}
	problemOf(t, c2.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")

	evs := e.pub.all()[before:]
	if len(evs) != 1 || evs[0].Type != events.SessionRevoked {
		t.Fatalf("events = %+v", evs)
	}
	p := evs[0].Payload.(events.SessionRevokedPayload)
	// pavel has three sessions: register (c1, replaced by login), login c1, login c2.
	if p.UserID != u.ID || len(p.SessionIDs) != 2 || slices.Contains(p.SessionIDs, keep) {
		t.Fatalf("payload = %+v, want the two sessions other than %v", p, keep)
	}

	problemOf(t, c2.Do(http.MethodPost, "/api/v1/auth/login", creds("pavel", "password123")),
		http.StatusUnauthorized, "invalid_credentials")
	if resp := c2.Do(http.MethodPost, "/api/v1/auth/login", creds("pavel", "newpassword1")); resp.Status != http.StatusOK {
		t.Fatalf("login with new password = %d", resp.Status)
	}
	if err := e.svc.ChangePassword(t.Context(), 777, "password123", "newpassword1", 0); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("unknown user: err = %v", err)
	}
}

func TestRevokeUserSessions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c1, c2 := e.client(), e.client()
	id := userID(t, register(t, c1, "rita", "password123"))
	c2.Do(http.MethodPost, "/api/v1/auth/login", creds("rita", "password123"))
	other := e.client()
	register(t, other, "sam", "password123")

	if err := e.svc.RevokeUserSessions(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	problemOf(t, c1.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	problemOf(t, c2.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	if resp := other.Do(http.MethodGet, "/api/v1/users/@me", nil); resp.Status != http.StatusOK {
		t.Fatalf("another user's session was revoked: %d", resp.Status)
	}
	evs := e.pub.all()
	last := evs[len(evs)-1]
	p := last.Payload.(events.SessionRevokedPayload)
	if last.Type != events.SessionRevoked || p.UserID != id || len(p.SessionIDs) != 2 {
		t.Fatalf("event = %+v", last)
	}

	// Nothing left to revoke: no event.
	n := len(e.pub.all())
	if err := e.svc.RevokeUserSessions(t.Context(), id); err != nil || len(e.pub.all()) != n {
		t.Fatalf("second revoke: err %v, events %d -> %d", err, n, len(e.pub.all()))
	}
	if err := e.svc.RevokeSession(t.Context(), 31337); err != nil || len(e.pub.all()) != n {
		t.Fatalf("revoke unknown session: err %v, events %d -> %d", err, n, len(e.pub.all()))
	}
}

func TestValidationErrorMapsToProblem(t *testing.T) {
	t.Parallel()
	err := &auth.ValidationError{Fields: []httpx.FieldError{{Field: "username", Message: "bad"}}}
	if !strings.Contains(err.Error(), "username") {
		t.Fatalf("Error() = %q", err.Error())
	}
}
