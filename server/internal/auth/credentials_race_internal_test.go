package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// The verification hook places a credential mutation exactly between reading
// the old hash and committing the operation that trusted it.
func TestLoginRejectsCredentialsChangedDuringVerification(t *testing.T) {
	for _, mutation := range []string{"reset", "change", "disable", "rehash after reset"} {
		t.Run(mutation, func(t *testing.T) {
			s := NewService(Options{Pool: testkit.Postgres(t), IDs: snowflake.NewGenerator(0, time.Now)})
			ctx := t.Context()
			u, token, err := s.Register(ctx, RegisterInput{Username: "racer", Password: "password123"}, SessionMeta{})
			if err != nil {
				t.Fatalf("set up credential race: %v", err)
			}
			_, keep, err := s.Authenticate(ctx, token)
			if err != nil {
				t.Fatalf("set up credential race: %v", err)
			}
			fired := false
			s.verify = func(ctx context.Context, pw, encoded string) (bool, bool, error) {
				ok, rehash, err := VerifyPassword(ctx, pw, encoded)
				if !fired && ok && err == nil {
					fired = true
					switch mutation {
					case "change":
						err = s.ChangePassword(ctx, u.ID, "password123", "password456", keep)
					case "disable":
						_, err = s.SetDisabled(ctx, u.ID, true)
					default:
						err = s.ResetPassword(ctx, u.ID, "password456")
					}
					if err != nil {
						t.Fatalf("set up credential race: %v", err)
					}
				}
				return ok, rehash || mutation == "rehash after reset", err
			}
			_, staleToken, err := s.Login(ctx, "racer", "password123", SessionMeta{})
			s.verify = VerifyPassword
			want := ErrInvalidCredentials
			if !errors.Is(err, want) || staleToken != "" {
				t.Errorf("stale Login = token present %v, %v; want %v", staleToken != "", err, want)
			}
			if mutation != "disable" {
				if _, _, err := s.Login(ctx, "racer", "password456", SessionMeta{}); err != nil {
					t.Errorf("replacement password no longer works: %v", err)
				}
				if _, _, err := s.Login(ctx, "racer", "password123", SessionMeta{}); !errors.Is(err, ErrInvalidCredentials) {
					t.Errorf("old password restored: %v", err)
				}
			}
		})
	}
}

func TestChangePasswordCannotOverwriteConcurrentReset(t *testing.T) {
	s := NewService(Options{Pool: testkit.Postgres(t), IDs: snowflake.NewGenerator(0, time.Now)})
	ctx := t.Context()
	u, _, err := s.Register(ctx, RegisterInput{Username: "racer", Password: "password123"}, SessionMeta{})
	if err != nil {
		t.Fatalf("set up credential race: %v", err)
	}
	s.verify = func(ctx context.Context, pw, encoded string) (bool, bool, error) {
		ok, rehash, err := VerifyPassword(ctx, pw, encoded)
		if err == nil && ok {
			if err := s.ResetPassword(ctx, u.ID, "resetpassword"); err != nil {
				t.Fatalf("set up credential race: %v", err)
			}
		}
		return ok, rehash, err
	}
	err = s.ChangePassword(ctx, u.ID, "password123", "stalechange", 0)
	s.verify = VerifyPassword
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("stale ChangePassword = %v", err)
	}
	if _, _, err := s.Login(ctx, "racer", "resetpassword", SessionMeta{}); err != nil {
		t.Errorf("reset password overwritten: %v", err)
	}
}

func TestPasswordRehashCannotOverwriteResetAfterSessionCreation(t *testing.T) {
	s := NewService(Options{Pool: testkit.Postgres(t), IDs: snowflake.NewGenerator(0, time.Now)})
	ctx := t.Context()
	u, _, err := s.Register(ctx, RegisterInput{Username: "racer", Password: "password123"}, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	verified, err := s.q.UserByID(ctx, s.pool, u.ID)
	if err != nil {
		t.Fatalf("load verified hash: %v", err)
	}
	if err := s.ResetPassword(ctx, u.ID, "resetpassword"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	s.upgradeHash(ctx, verified, "password123")
	if _, _, err := s.Login(ctx, "racer", "resetpassword", SessionMeta{}); err != nil {
		t.Errorf("Login with reset password: %v", err)
	}
}
