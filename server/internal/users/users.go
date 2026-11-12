// Package users serves the profile of the signed-in user (display name,
// "about me", status, password and avatar) and the profiles of the people
// they know. Accounts belong to the auth module; this package
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

// Profile writes per user (display name and avatar together): 10 per 10
// minutes.
var (
	profileRate  = rate.Every(time.Minute)
	profileBurst = 10
	profileTTL   = 20 * time.Minute
)

// Options configures a Service. Accounts and Files are required.
type Options struct {
	Accounts  auth.Accounts
	Files     files.Store
	Publisher events.Publisher // nil: events are dropped
	// Peers tells whose profile a user may see; nil hides every profile
	// but the user's own.
	Peers Peers
	// PasswordLimiter limits password changes per user; nil takes the
	// default (5 per 15 minutes).
	PasswordLimiter *ratelimit.Limiter
	// ProfileLimiter limits profile writes per user (PATCH @me, PUT and
	// DELETE avatar together); nil takes the default (10 per 10 minutes).
	ProfileLimiter *ratelimit.Limiter
}

// Peers is the part of guilds.Service the profiles need.
type Peers interface {
	// ProfileLink returns the guilds viewer shares with target and whether
	// viewer may see target's profile.
	ProfileLink(ctx context.Context, viewer, target snowflake.ID) (mutual []snowflake.ID, visible bool, err error)
}

// Service implements the profile operations.
type Service struct {
	peers          Peers
	accounts       auth.Accounts
	files          files.Store
	pub            events.Publisher
	pwLimiter      *ratelimit.Limiter
	profileLimiter *ratelimit.Limiter
	// avatarLocks serialize the avatar changes of one user (striped by
	// ID), so that a replaced file is never deleted after a concurrent
	// upload of the same image made it current again.
	avatarLocks [64]sync.Mutex
}

// NewService builds a Service from o.
func NewService(o Options) *Service {
	s := &Service{peers: o.Peers, accounts: o.Accounts, files: o.Files, pub: o.Publisher, pwLimiter: o.PasswordLimiter,
		profileLimiter: o.ProfileLimiter}
	if s.pwLimiter == nil {
		s.pwLimiter = ratelimit.New(passwordRate, passwordBurst, passwordTTL)
	}
	if s.profileLimiter == nil {
		s.profileLimiter = ratelimit.New(profileRate, profileBurst, profileTTL)
	}
	return s
}

func (s *Service) avatarLock(id snowflake.ID) *sync.Mutex {
	return &s.avatarLocks[uint64(id)%uint64(len(s.avatarLocks))] //nolint:gosec // G115: IDs are positive
}

// publishUpdate announces a changed profile; call it after the change
// committed. The user's own connections get every field (CurrentUser);
// peers get the public ones, and only when one of those changed. A new
// status or status line goes to the gateway, which tells the peers what
// they now see.
func (s *Service) publishUpdate(ctx context.Context, before, u auth.User) {
	if s.pub == nil {
		return
	}
	self, prev := u.Self(), before.Self()
	if !samePublic(self.PublicUser, prev.PublicUser) {
		s.pub.Publish(ctx, events.Event{
			Type:          events.UserUpdate,
			Audience:      events.Audience{Kind: events.AudiencePeers, UserIDs: []snowflake.ID{u.ID}},
			ExcludeUserID: u.ID,
			Payload:       u.Public(),
		})
	}
	if !samePublic(self.PublicUser, prev.PublicUser) || self.Bio != prev.Bio || self.Status != prev.Status ||
		!sameLine(self.CustomStatus, prev.CustomStatus) {
		s.pub.Publish(ctx, events.Event{
			Type:     events.UserUpdate,
			Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{u.ID}},
			Payload:  self,
		})
	}
	if u.Status != before.Status || !sameLine(u.CustomStatus, before.CustomStatus) {
		s.pub.Publish(ctx, events.Event{
			Type:     events.PresenceSet,
			Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{u.ID}},
			Payload:  events.PresenceSetPayload{UserID: u.ID, Status: u.Status, CustomStatus: u.CustomStatus},
		})
	}
}

func samePublic(a, b auth.PublicUser) bool {
	return a.ID == b.ID && a.Username == b.Username && a.DisplayName == b.DisplayName &&
		sameLine(a.AvatarURL, b.AvatarURL) && a.CreatedAt == b.CreatedAt
}

func sameLine(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// UpdateProfile writes the fields c sets (see auth.Accounts.ChangeProfile)
// and announces what changed.
func (s *Service) UpdateProfile(ctx context.Context, current auth.User, c auth.ProfileChange) (auth.User, error) {
	u, err := s.accounts.ChangeProfile(ctx, current.ID, c)
	if err != nil {
		return auth.User{}, err
	}
	s.publishUpdate(ctx, current, u)
	return u, nil
}

// Profile is what one user sees of another (schema UserProfile).
type Profile struct {
	User           auth.PublicUser `json:"user"`
	Bio            string          `json:"bio"`
	MutualGuildIDs []snowflake.ID  `json:"mutual_guild_ids"`
}

// GetProfile returns target's profile as viewer sees it, or
// auth.ErrUserNotFound when viewer may not see it (they share no guild,
// friendship, pending request or direct message) or there is no such user.
func (s *Service) GetProfile(ctx context.Context, viewer, target snowflake.ID) (Profile, error) {
	mutual, visible := []snowflake.ID{}, viewer == target
	if s.peers != nil {
		m, v, err := s.peers.ProfileLink(ctx, viewer, target)
		if err != nil {
			return Profile{}, err
		}
		mutual, visible = m, v || visible
	}
	if !visible {
		return Profile{}, auth.ErrUserNotFound
	}
	u, err := s.accounts.GetUser(ctx, target)
	if err != nil {
		return Profile{}, err
	}
	if mutual == nil {
		mutual = []snowflake.ID{}
	}
	return Profile{User: u.Public(), Bio: u.Bio, MutualGuildIDs: mutual}, nil
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
// announces the change. Once the file is written the account update runs to
// the end even if the client goes away, so file and account stay in step:
// the replaced file is deleted after the update, and on a failed update
// the new file is (unless it is the current avatar already). Failed
// deletions are only logged.
func (s *Service) SetAvatar(ctx context.Context, userID snowflake.ID, image []byte) (auth.User, error) {
	mu := s.avatarLock(userID)
	mu.Lock()
	defer mu.Unlock()

	before, err := s.accounts.GetUser(ctx, userID)
	if err != nil {
		return auth.User{}, fmt.Errorf("users: load user: %w", err)
	}
	hash, err := s.files.SaveImage(ctx, files.KindAvatar, userID, bytes.NewReader(image))
	if err != nil {
		return auth.User{}, err
	}
	u, prev, err := s.accounts.SetAvatar(context.WithoutCancel(ctx), userID, &hash)
	if err != nil {
		if before.AvatarHash == nil || *before.AvatarHash != hash {
			s.dropFile(ctx, userID, &hash)
		}
		return auth.User{}, fmt.Errorf("users: set avatar: %w", err)
	}
	if prev == nil || *prev != hash {
		s.dropFile(ctx, userID, prev)
		s.publishUpdate(ctx, before, u)
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
		before := u
		before.AvatarHash = prev
		s.publishUpdate(ctx, before, u)
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
