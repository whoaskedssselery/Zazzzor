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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/admin/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
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
	ResetPassword(ctx context.Context, userID snowflake.ID, next string) error
	SetDisabled(ctx context.Context, userID snowflake.ID, disabled bool) (auth.User, error)
}

var _ Accounts = (*auth.Service)(nil)

// Service implements the admin operations.
type Service struct {
	settings *Settings
	accounts Accounts
}

// NewService builds a Service.
func NewService(settings *Settings, accounts Accounts) *Service {
	return &Service{settings: settings, accounts: accounts}
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
