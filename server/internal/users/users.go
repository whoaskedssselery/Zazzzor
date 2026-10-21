// Package users serves the profile of the signed-in user: display name,
// password and avatar. Accounts belong to the auth module; this package
// changes them only through auth.Accounts, and stores avatar images through
// files.Store.
package users

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Password changes per user: 5 per 15 minutes.
var (
	passwordRate  = rate.Every(15 * time.Minute / 5)
	passwordBurst = 5
	passwordTTL   = 30 * time.Minute
)

// Options configures a Service. Accounts and Files are required.
type Options struct {
	Accounts  auth.Accounts
	Files     files.Store
	Publisher events.Publisher // nil: events are dropped
	// PasswordLimiter limits password changes per user; nil takes the
	// default (5 per 15 minutes).
	PasswordLimiter *ratelimit.Limiter
}

// Service implements the profile operations.
type Service struct {
	accounts  auth.Accounts
	files     files.Store
	pub       events.Publisher
	pwLimiter *ratelimit.Limiter
	// avatarLocks serialize the avatar changes of one user (striped by
	// ID), so that a replaced file is never deleted after a concurrent
	// upload of the same image made it current again.
	avatarLocks [64]sync.Mutex
}

// NewService builds a Service from o.
func NewService(o Options) *Service {
	s := &Service{accounts: o.Accounts, files: o.Files, pub: o.Publisher, pwLimiter: o.PasswordLimiter}
	if s.pwLimiter == nil {
		s.pwLimiter = ratelimit.New(passwordRate, passwordBurst, passwordTTL)
	}
	return s
}

func (s *Service) avatarLock(id snowflake.ID) *sync.Mutex {
	return &s.avatarLocks[uint64(id)%uint64(len(s.avatarLocks))] //nolint:gosec // G115: IDs are positive
}

// publishUpdate announces a changed profile; call it after the change
// committed.
func (s *Service) publishUpdate(ctx context.Context, u auth.User) {
	if s.pub == nil {
		return
	}
	s.pub.Publish(ctx, events.Event{
		Type:     events.UserUpdate,
		Audience: events.Audience{Kind: events.AudiencePeers, UserIDs: []snowflake.ID{u.ID}},
		Payload:  u.Public(),
	})
}

// UpdateDisplayName sets the display name (see auth.NormalizeDisplayName)
// and announces it when it changed.
func (s *Service) UpdateDisplayName(ctx context.Context, current auth.User, displayName string) (auth.User, error) {
	u, err := s.accounts.UpdateProfile(ctx, current.ID, displayName)
	if err != nil {
		return auth.User{}, err
	}
	if u.DisplayName != current.DisplayName {
		s.publishUpdate(ctx, u)
	}
	return u, nil
}

// ErrWrongPassword is returned by ChangePassword for a wrong current
// password.
var ErrWrongPassword = errors.New("users: wrong current password")

// ChangePassword checks current, sets next and signs out every other session
// of the user. Each call counts against the per-user limit.
func (s *Service) ChangePassword(ctx context.Context, userID, sessionID snowflake.ID, current, next string) error {
	if ok, retry := s.pwLimiter.Allow(userID.String()); !ok {
		return &auth.RateLimitedError{RetryAfter: retry}
	}
	err := s.accounts.ChangePassword(ctx, userID, current, next, sessionID)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		return ErrWrongPassword
	}
	return err
}

// SetAvatar stores image (the uploaded bytes) as the user's avatar and
// announces the change. The replaced image file is deleted once the account
// points at the new one; failing that is only logged.
func (s *Service) SetAvatar(ctx context.Context, userID snowflake.ID, image []byte) (auth.User, error) {
	mu := s.avatarLock(userID)
	mu.Lock()
	defer mu.Unlock()

	hash, err := s.files.SaveImage(ctx, files.KindAvatar, userID, bytes.NewReader(image))
	if err != nil {
		return auth.User{}, err
	}
	// On failure the new file stays: it may be the current avatar already
	// (same image), and an orphan costs only disk space.
	u, prev, err := s.accounts.SetAvatar(ctx, userID, &hash)
	if err != nil {
		return auth.User{}, fmt.Errorf("users: set avatar: %w", err)
	}
	if prev == nil || *prev != hash {
		s.dropFile(ctx, userID, prev)
		s.publishUpdate(ctx, u)
	}
	return u, nil
}

// ClearAvatar removes the avatar, if any.
func (s *Service) ClearAvatar(ctx context.Context, userID snowflake.ID) (auth.User, error) {
	mu := s.avatarLock(userID)
	mu.Lock()
	defer mu.Unlock()

	u, prev, err := s.accounts.SetAvatar(ctx, userID, nil)
	if err != nil {
		return auth.User{}, fmt.Errorf("users: clear avatar: %w", err)
	}
	if prev != nil {
		s.dropFile(ctx, userID, prev)
		s.publishUpdate(ctx, u)
	}
	return u, nil
}

func (s *Service) dropFile(ctx context.Context, userID snowflake.ID, hash *string) {
	if hash == nil {
		return
	}
	if err := s.files.Delete(files.KindAvatar, userID, *hash); err != nil {
		httpx.LoggerFrom(ctx).WarnContext(ctx, "delete replaced avatar",
			"user_id", userID.String(), "hash", *hash, "err", err.Error())
	}
}
