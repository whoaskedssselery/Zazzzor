// Package auth owns accounts and sessions: password hashing, registration,
// login, cookie sessions and the middleware that resolves the current user.
// The users and sessions tables belong to it; other modules reach accounts
// through Accounts.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Errors returned by Service. ToProblem maps them to HTTP problems.
var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrAccountDisabled    = errors.New("auth: account disabled")
	ErrUsernameTaken      = errors.New("auth: username taken")
	ErrRegistrationClosed = errors.New("auth: registration closed")
	ErrUnauthenticated    = errors.New("auth: no valid session")
	ErrUserNotFound       = errors.New("auth: user not found")
)

// ValidationError lists the request fields that failed validation.
type ValidationError struct {
	Fields []httpx.FieldError
}

func (e *ValidationError) Error() string {
	names := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		names[i] = f.Field
	}
	return "auth: invalid " + strings.Join(names, ", ")
}

// ToProblem maps the errors of this package to their HTTP problem; any other
// error is returned unchanged (and becomes 500 in httpx.Wrap).
func ToProblem(err error) error {
	if verr, ok := errors.AsType[*ValidationError](err); ok {
		return httpx.BadRequest("validation_failed", "some fields are invalid", verr.Fields...)
	}
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return httpx.Unauthorized("invalid_credentials")
	case errors.Is(err, ErrAccountDisabled):
		return httpx.Forbidden("account_disabled")
	case errors.Is(err, ErrUsernameTaken):
		return httpx.Conflict("username_taken", "this username is already taken")
	case errors.Is(err, ErrRegistrationClosed):
		return httpx.Forbidden("registration_closed")
	case errors.Is(err, ErrUnauthenticated):
		return httpx.Unauthorized("unauthenticated")
	case errors.Is(err, ErrUserNotFound):
		return httpx.NotFound()
	}
	return err
}

// User is an account as the rest of the server sees it. The password hash
// never leaves this package.
type User struct {
	ID          snowflake.ID
	Username    string
	DisplayName string
	AvatarHash  *string
	CreatedAt   time.Time
	DisabledAt  *time.Time
	// IsAdmin is true for usernames listed in ZAZZZOR_ADMIN_USERNAMES.
	IsAdmin bool
}

// PublicUser is the JSON form of a User (schema User in api/openapi.yaml).
type PublicUser struct {
	ID          snowflake.ID `json:"id"`
	Username    string       `json:"username"`
	DisplayName string       `json:"display_name"`
	AvatarURL   *string      `json:"avatar_url"`
	CreatedAt   string       `json:"created_at"`
}

// Public returns the JSON form of u.
func (u User) Public() PublicUser {
	p := PublicUser{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		CreatedAt:   httpx.FormatTime(u.CreatedAt),
	}
	if u.AvatarHash != nil {
		url := "/api/v1/files/avatars/" + u.ID.String() + "/" + *u.AvatarHash + ".png"
		p.AvatarURL = &url
	}
	return p
}

// Accounts is what other modules (users, admin) use to read and change
// accounts; the users table is only queried here.
type Accounts interface {
	GetUser(ctx context.Context, userID snowflake.ID) (User, error)
	UsersByIDs(ctx context.Context, ids []snowflake.ID) ([]User, error)
	UpdateProfile(ctx context.Context, userID snowflake.ID, displayName string) (User, error)
	// SetAvatar sets (hash = 16 lower-case hex digits) or clears (nil) the avatar.
	SetAvatar(ctx context.Context, userID snowflake.ID, hash *string) (User, error)
	// ChangePassword checks current, sets next and revokes every session of
	// the user except keepSessionID.
	ChangePassword(ctx context.Context, userID snowflake.ID, current, next string, keepSessionID snowflake.ID) error
}

var _ Accounts = (*Service)(nil)

// Default rate limits per client IP.
var (
	loginRate      = rate.Every(time.Minute / 10) // 10 per minute
	loginBurst     = 10
	registerRate   = rate.Every(time.Hour / 3) // 3 per hour
	registerBurst  = 3
	loginLimitTTL  = 10 * time.Minute
	registerLimTTL = 2 * time.Hour
)

// Options configures a Service. Pool and IDs are required.
type Options struct {
	Pool      *pgxpool.Pool
	IDs       *snowflake.Generator
	Publisher events.Publisher // nil: events are dropped
	// AdminUsernames are matched case-insensitively.
	AdminUsernames []string
	// CookieSecure sets the Secure attribute of the session cookie.
	CookieSecure bool
	// TrustedProxies decide whether X-Forwarded-For names the client.
	TrustedProxies []netip.Prefix
	// RegistrationOpen reports whether new accounts may be created; nil
	// means always.
	RegistrationOpen func(context.Context) (bool, error)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Per-IP limiters; nil takes the defaults (login 10/min, register 3/h).
	LoginLimiter, RegisterLimiter *ratelimit.Limiter
}

// Service implements registration, login and sessions.
type Service struct {
	pool             *pgxpool.Pool
	q                *store.Queries
	ids              *snowflake.Generator
	pub              events.Publisher
	admins           map[string]bool
	cookieSecure     bool
	trusted          []netip.Prefix
	registrationOpen func(context.Context) (bool, error)
	now              func() time.Time
	loginLimiter     *ratelimit.Limiter
	registerLimiter  *ratelimit.Limiter
	verify           func(pw, encoded string) (ok, needsRehash bool, err error)
}

// NewService builds a Service from o.
func NewService(o Options) *Service {
	s := &Service{
		pool:             o.Pool,
		q:                store.New(),
		ids:              o.IDs,
		pub:              o.Publisher,
		admins:           make(map[string]bool, len(o.AdminUsernames)),
		cookieSecure:     o.CookieSecure,
		trusted:          o.TrustedProxies,
		registrationOpen: o.RegistrationOpen,
		now:              o.Now,
		loginLimiter:     o.LoginLimiter,
		registerLimiter:  o.RegisterLimiter,
		verify:           VerifyPassword,
	}
	for _, name := range o.AdminUsernames {
		s.admins[strings.ToLower(name)] = true
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.loginLimiter == nil {
		s.loginLimiter = ratelimit.New(loginRate, loginBurst, loginLimitTTL)
	}
	if s.registerLimiter == nil {
		s.registerLimiter = ratelimit.New(registerRate, registerBurst, registerLimTTL)
	}
	_ = dummyHash() // ready before the first login
	return s
}

func (s *Service) toUser(u store.User) User {
	return User{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		AvatarHash:  u.AvatarHash,
		CreatedAt:   u.CreatedAt,
		DisabledAt:  u.DisabledAt,
		IsAdmin:     s.admins[u.Username],
	}
}

// SessionMeta describes the client a session is created for.
type SessionMeta struct {
	UserAgent string
	IP        netip.Addr
}

// RegisterInput is a registration request; DisplayName nil means "same as
// the username as typed".
type RegisterInput struct {
	Username    string
	Password    string
	DisplayName *string
}

type validRegistration struct {
	username, password, displayName string
}

var usernameRE = regexp.MustCompile(`^[A-Za-z0-9_.]{2,32}$`)

// normalizeUsername lower-cases a username that has the allowed shape. ASCII
// is checked before lower-casing, so look-alikes such as the Kelvin sign
// cannot fold into a valid name.
func normalizeUsername(s string) (string, bool) {
	if !usernameRE.MatchString(s) {
		return "", false
	}
	return strings.ToLower(s), true
}

func passwordProblem(pw string) string {
	if n := utf8.RuneCountInString(pw); n < 8 || n > 128 {
		return "must be 8 to 128 characters"
	}
	return ""
}

// NormalizeDisplayName trims spaces and invisible format characters from the
// ends of s and checks it is 1 to 32 characters without control characters.
func NormalizeDisplayName(s string) (string, error) {
	t := strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
	var msg string
	switch n := utf8.RuneCountInString(t); {
	case n == 0:
		msg = "must not be empty"
	case n > 32:
		msg = "must be at most 32 characters"
	case strings.ContainsFunc(t, unicode.IsControl):
		msg = "must not contain control characters"
	default:
		return t, nil
	}
	return "", &ValidationError{Fields: []httpx.FieldError{{Field: "display_name", Message: msg}}}
}

func validateRegistration(in RegisterInput) (validRegistration, error) {
	var v validRegistration
	var fields []httpx.FieldError
	name, ok := normalizeUsername(in.Username)
	if !ok {
		fields = append(fields, httpx.FieldError{Field: "username",
			Message: "must be 2 to 32 characters: latin letters, digits, _ and ."})
	}
	if msg := passwordProblem(in.Password); msg != "" {
		fields = append(fields, httpx.FieldError{Field: "password", Message: msg})
	}
	dn := in.Username
	if in.DisplayName != nil {
		dn = *in.DisplayName
	}
	dn, err := NormalizeDisplayName(dn)
	if verr, isVerr := errors.AsType[*ValidationError](err); isVerr && (in.DisplayName != nil || ok) {
		fields = append(fields, verr.Fields...)
	}
	if len(fields) > 0 {
		return v, &ValidationError{Fields: fields}
	}
	return validRegistration{username: name, password: in.Password, displayName: dn}, nil
}

func (s *Service) checkRegistrationOpen(ctx context.Context) error {
	if s.registrationOpen == nil {
		return nil
	}
	open, err := s.registrationOpen(ctx)
	if err != nil {
		return fmt.Errorf("auth: registration setting: %w", err)
	}
	if !open {
		return ErrRegistrationClosed
	}
	return nil
}

// Register creates an account and its first session, returning the session
// token for the cookie.
func (s *Service) Register(ctx context.Context, in RegisterInput, meta SessionMeta) (User, string, error) {
	if err := s.checkRegistrationOpen(ctx); err != nil {
		return User{}, "", err
	}
	v, err := validateRegistration(in)
	if err != nil {
		return User{}, "", err
	}
	return s.register(ctx, v, meta)
}

func (s *Service) register(ctx context.Context, v validRegistration, meta SessionMeta) (User, string, error) {
	hash, err := HashPassword(v.password)
	if err != nil {
		return User{}, "", err
	}
	now := s.now()
	var row store.User
	var token string
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var txErr error
		row, txErr = s.q.CreateUser(ctx, tx, store.CreateUserParams{
			ID:           s.ids.Next(),
			Username:     v.username,
			DisplayName:  v.displayName,
			PasswordHash: hash,
			CreatedAt:    now,
		})
		if errors.Is(txErr, pgx.ErrNoRows) {
			return ErrUsernameTaken
		}
		if txErr != nil {
			return fmt.Errorf("auth: create user: %w", txErr)
		}
		token, txErr = s.createSession(ctx, tx, row.ID, meta, now)
		return txErr
	})
	if err != nil {
		return User{}, "", err
	}
	return s.toUser(row), token, nil
}

// Login checks the credentials and opens a session. Unknown users, malformed
// usernames and wrong passwords all return ErrInvalidCredentials after one
// full Argon2id verification, so neither the answer nor its timing tells
// them apart. A disabled account is reported only after a correct password.
func (s *Service) Login(ctx context.Context, username, password string, meta SessionMeta) (User, string, error) {
	var row store.User
	found := false
	if name, ok := normalizeUsername(username); ok {
		var err error
		row, err = s.q.UserByUsername(ctx, s.pool, name)
		switch {
		case err == nil:
			found = true
		case !errors.Is(err, pgx.ErrNoRows):
			return User{}, "", fmt.Errorf("auth: load user: %w", err)
		}
	}
	if !found {
		_, _, _ = s.verify(password, dummyHash())
		return User{}, "", ErrInvalidCredentials
	}

	ok, rehash, err := s.verify(password, row.PasswordHash)
	if err != nil {
		return User{}, "", fmt.Errorf("auth: stored hash of user %d: %w", row.ID, err)
	}
	if !ok {
		return User{}, "", ErrInvalidCredentials
	}
	if row.DisabledAt != nil {
		return User{}, "", ErrAccountDisabled
	}
	if rehash {
		s.upgradeHash(ctx, row.ID, password)
	}
	token, err := s.createSession(ctx, s.pool, row.ID, meta, s.now())
	if err != nil {
		return User{}, "", err
	}
	return s.toUser(row), token, nil
}

// upgradeHash re-hashes a password stored with old parameters. Failure only
// postpones the upgrade to the next login.
func (s *Service) upgradeHash(ctx context.Context, userID snowflake.ID, password string) {
	hash, err := HashPassword(password)
	if err == nil {
		_, err = s.q.SetPasswordHash(ctx, s.pool, store.SetPasswordHashParams{ID: userID, PasswordHash: hash})
	}
	if err != nil {
		httpx.LoggerFrom(ctx).WarnContext(ctx, "upgrade password hash", "user_id", userID.String(), "err", err.Error())
	}
}

func (s *Service) createSession(ctx context.Context, tx store.DBTX, userID snowflake.ID, meta SessionMeta, now time.Time) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	if err = s.q.DeleteExpiredUserSessions(ctx, tx, store.DeleteExpiredUserSessionsParams{UserID: userID, Now: now}); err != nil {
		return "", fmt.Errorf("auth: purge expired sessions: %w", err)
	}
	var ua *string
	if meta.UserAgent != "" {
		v := strings.ToValidUTF8(truncateUTF8(meta.UserAgent, maxUserAgent), "")
		ua = &v
	}
	var ip *netip.Addr
	if meta.IP.IsValid() {
		a := meta.IP.WithZone("")
		ip = &a
	}
	err = s.q.CreateSession(ctx, tx, store.CreateSessionParams{
		ID:        s.ids.Next(),
		UserID:    userID,
		TokenHash: hash,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
		UserAgent: ua,
		Ip:        ip,
	})
	if err != nil {
		return "", fmt.Errorf("auth: create session: %w", err)
	}
	return token, nil
}

type session struct {
	id         snowflake.ID
	user       User
	lastUsedAt time.Time
}

func (s *Service) lookup(ctx context.Context, rawToken string) (session, error) {
	if !wellFormedToken(rawToken) {
		return session{}, ErrUnauthenticated
	}
	row, err := s.q.SessionByTokenHash(ctx, s.pool, store.SessionByTokenHashParams{
		TokenHash: hashToken(rawToken),
		Now:       s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return session{}, ErrUnauthenticated
	}
	if err != nil {
		return session{}, fmt.Errorf("auth: load session: %w", err)
	}
	return session{id: row.SessionID, user: s.toUser(row.User), lastUsedAt: row.LastUsedAt}, nil
}

// Authenticate resolves a session token to its user and session ID. Missing,
// malformed, expired or revoked tokens and tokens of disabled accounts give
// ErrUnauthenticated.
func (s *Service) Authenticate(ctx context.Context, rawToken string) (User, snowflake.ID, error) {
	sess, err := s.lookup(ctx, rawToken)
	if err != nil {
		return User{}, 0, err
	}
	return sess.user, sess.id, nil
}

// renew slides the session's expiry if it was last renewed a day or more
// ago. It reports whether this call renewed it.
func (s *Service) renew(ctx context.Context, sess session) (bool, error) {
	now := s.now()
	if now.Sub(sess.lastUsedAt) < renewEvery {
		return false, nil
	}
	n, err := s.q.TouchSession(ctx, s.pool, store.TouchSessionParams{
		ID:             sess.id,
		Now:            now,
		ExpiresAt:      now.Add(SessionTTL),
		PrevLastUsedAt: sess.lastUsedAt,
	})
	if err != nil {
		return false, fmt.Errorf("auth: renew session: %w", err)
	}
	return n == 1, nil
}

// RevokeSession deletes one session and announces it. Unknown IDs are a no-op.
func (s *Service) RevokeSession(ctx context.Context, sessionID snowflake.ID) error {
	userID, err := s.q.DeleteSession(ctx, s.pool, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	s.publishRevoked(ctx, userID, []snowflake.ID{sessionID})
	return nil
}

// RevokeUserSessions deletes every session of a user and announces them.
func (s *Service) RevokeUserSessions(ctx context.Context, userID snowflake.ID) error {
	ids, err := s.q.DeleteUserSessions(ctx, s.pool, userID)
	if err != nil {
		return fmt.Errorf("auth: delete sessions: %w", err)
	}
	s.publishRevoked(ctx, userID, ids)
	return nil
}

// publishRevoked must be called after the deleting statement committed.
func (s *Service) publishRevoked(ctx context.Context, userID snowflake.ID, ids []snowflake.ID) {
	if s.pub == nil || len(ids) == 0 {
		return
	}
	s.pub.Publish(ctx, events.Event{
		Type:     events.SessionRevoked,
		Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{userID}},
		Payload:  events.SessionRevokedPayload{UserID: userID, SessionIDs: ids},
	})
}

// GetUser returns one account or ErrUserNotFound.
func (s *Service) GetUser(ctx context.Context, userID snowflake.ID) (User, error) {
	row, err := s.q.UserByID(ctx, s.pool, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: load user: %w", err)
	}
	return s.toUser(row), nil
}

// UsersByIDs returns the existing accounts among ids, ordered by ID, each
// once.
func (s *Service) UsersByIDs(ctx context.Context, ids []snowflake.ID) ([]User, error) {
	if len(ids) == 0 {
		return []User{}, nil
	}
	raw := make([]int64, len(ids))
	for i, id := range ids {
		raw[i] = int64(id)
	}
	rows, err := s.q.UsersByIDs(ctx, s.pool, raw)
	if err != nil {
		return nil, fmt.Errorf("auth: load users: %w", err)
	}
	out := make([]User, len(rows))
	for i, r := range rows {
		out[i] = s.toUser(r)
	}
	return out, nil
}

// UpdateProfile sets the display name (validated like at registration).
func (s *Service) UpdateProfile(ctx context.Context, userID snowflake.ID, displayName string) (User, error) {
	dn, err := NormalizeDisplayName(displayName)
	if err != nil {
		return User{}, err
	}
	row, err := s.q.SetDisplayName(ctx, s.pool, store.SetDisplayNameParams{ID: userID, DisplayName: dn})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: update display name: %w", err)
	}
	return s.toUser(row), nil
}

var avatarHashRE = regexp.MustCompile(`^[0-9a-f]{16}$`)

// SetAvatar stores or clears the avatar hash.
func (s *Service) SetAvatar(ctx context.Context, userID snowflake.ID, hash *string) (User, error) {
	if hash != nil && !avatarHashRE.MatchString(*hash) {
		return User{}, fmt.Errorf("auth: avatar hash %q is not 16 hex digits", *hash)
	}
	row, err := s.q.SetAvatarHash(ctx, s.pool, store.SetAvatarHashParams{ID: userID, AvatarHash: hash})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: update avatar: %w", err)
	}
	return s.toUser(row), nil
}

// ChangePassword verifies current exactly like Login does (an unknown user
// costs the same dummy verification), sets next and revokes every other
// session of the user. A wrong current password is ErrInvalidCredentials; an
// invalid next password is a ValidationError on new_password.
func (s *Service) ChangePassword(ctx context.Context, userID snowflake.ID, current, next string, keepSessionID snowflake.ID) error {
	if msg := passwordProblem(next); msg != "" {
		return &ValidationError{Fields: []httpx.FieldError{{Field: "new_password", Message: msg}}}
	}
	row, err := s.q.UserByID(ctx, s.pool, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _, _ = s.verify(current, dummyHash())
		return ErrInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("auth: load user: %w", err)
	}
	ok, _, err := s.verify(current, row.PasswordHash)
	if err != nil {
		return fmt.Errorf("auth: stored hash of user %d: %w", row.ID, err)
	}
	if !ok {
		return ErrInvalidCredentials
	}

	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	var revoked []snowflake.ID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		n, txErr := s.q.SetPasswordHash(ctx, tx, store.SetPasswordHashParams{ID: userID, PasswordHash: hash})
		if txErr != nil {
			return fmt.Errorf("auth: set password: %w", txErr)
		}
		if n == 0 {
			return ErrUserNotFound
		}
		revoked, txErr = s.q.DeleteOtherUserSessions(ctx, tx, store.DeleteOtherUserSessionsParams{UserID: userID, KeepID: keepSessionID})
		if txErr != nil {
			return fmt.Errorf("auth: revoke sessions: %w", txErr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publishRevoked(ctx, userID, revoked)
	return nil
}
