// Package admin administers the instance: its settings (the instance table,
// owned here) and, through the auth module, password resets and disabling
// accounts. Only users listed in ZAZZZOR_ADMIN_USERNAMES (auth.User.IsAdmin)
// may use it.
package admin

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/admin/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Settings reads and writes the instance settings. It is separate from
// Service because auth needs RegistrationOpen before Service can be built.
type Settings struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

// NewSettings returns the settings stored in pool.
func NewSettings(pool *pgxpool.Pool) *Settings {
	return &Settings{pool: pool, q: store.New()}
}

// RegistrationOpen reports whether new accounts may register. It reads the
// database on every call (one query per registration request); it fits
// auth.Options.RegistrationOpen.
func (s *Settings) RegistrationOpen(ctx context.Context) (bool, error) {
	open, err := s.q.RegistrationOpen(ctx, s.pool)
	if err != nil {
		return false, fmt.Errorf("admin: read settings: %w", err)
	}
	return open, nil
}

// SetRegistrationOpen opens or closes registration.
func (s *Settings) SetRegistrationOpen(ctx context.Context, open bool) (bool, error) {
	got, err := s.q.SetRegistrationOpen(ctx, s.pool, open)
	if err != nil {
		return false, fmt.Errorf("admin: write settings: %w", err)
	}
	return got, nil
}

// Accounts is what admin needs from the auth module.
type Accounts interface {
	GetUserByUsername(ctx context.Context, username string) (auth.User, error)
	ResetPassword(ctx context.Context, userID snowflake.ID, next string) error
	SetDisabled(ctx context.Context, userID snowflake.ID, disabled bool) (auth.User, error)
}

var _ Accounts = (*auth.Service)(nil)

// Admin requests per administrator, all routes together: 30 per minute.
var (
	adminRate  = rate.Every(time.Minute / 30)
	adminBurst = 30
	adminTTL   = 2 * time.Minute
)

// Options configures a Service. Settings and Accounts are required.
type Options struct {
	Settings *Settings
	Accounts Accounts
	// Limiter limits the admin requests of each administrator; nil takes
	// the default (30 per minute).
	Limiter *ratelimit.Limiter
}

// Service implements the admin operations.
type Service struct {
	settings *Settings
	accounts Accounts
	limiter  *ratelimit.Limiter
}

// NewService builds a Service from o.
func NewService(o Options) *Service {
	s := &Service{settings: o.Settings, accounts: o.Accounts, limiter: o.Limiter}
	if s.limiter == nil {
		s.limiter = ratelimit.New(adminRate, adminBurst, adminTTL)
	}
	return s
}

// FindUser returns the account with that username (case-insensitively),
// or auth.ErrUserNotFound.
func (s *Service) FindUser(ctx context.Context, username string) (auth.User, error) {
	return s.accounts.GetUserByUsername(ctx, username)
}

// ErrCannotDisableSelf is returned when an administrator tries to disable
// their own account.
var ErrCannotDisableSelf = errors.New("admin: cannot disable own account")

// tempAlphabet leaves out look-alikes (0/O, 1/l/I) so the password can be
// read out to the user.
const (
	tempAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	tempLength   = 16
)

// temporaryPassword returns tempLength characters drawn uniformly from
// tempAlphabet with crypto/rand (about 93 bits).
func temporaryPassword() (string, error) {
	out := make([]byte, tempLength)
	n := big.NewInt(int64(len(tempAlphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("admin: random: %w", err)
		}
		out[i] = tempAlphabet[v.Int64()]
	}
	return string(out), nil
}

// ResetPassword gives the user a fresh temporary password, signs them out
// everywhere and returns the password.
func (s *Service) ResetPassword(ctx context.Context, userID snowflake.ID) (string, error) {
	pw, err := temporaryPassword()
	if err != nil {
		return "", err
	}
	if err := s.accounts.ResetPassword(ctx, userID, pw); err != nil {
		return "", err
	}
	return pw, nil
}

// SetDisabled disables (signing the user out everywhere) or enables an
// account. actor is the administrator doing it.
func (s *Service) SetDisabled(ctx context.Context, actor auth.User, userID snowflake.ID, disabled bool) error {
	if disabled && userID == actor.ID {
		return ErrCannotDisableSelf
	}
	_, err := s.accounts.SetDisabled(ctx, userID, disabled)
	return err
}
