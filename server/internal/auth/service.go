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
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/textx"
)

// Errors returned by Service. ToProblem maps them to HTTP problems.
var (
	ErrInvalidCredentials     = errors.New("auth: invalid credentials")
	ErrAccountDisabled        = errors.New("auth: account disabled")
	ErrUsernameTaken          = errors.New("auth: username taken")
	ErrAdminUsernameNotListed = errors.New("auth: username is not listed in ZAZZZOR_ADMIN_USERNAMES")
	ErrRegistrationClosed     = errors.New("auth: registration closed")
	ErrUnauthenticated        = errors.New("auth: no valid session")
	ErrUserNotFound           = errors.New("auth: user not found")
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
	if rl, ok := errors.AsType[*RateLimitedError](err); ok {
		return httpx.TooManyRequests(rl.RetryAfter)
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
	case errors.Is(err, ErrBusy):
		return httpx.ServiceUnavailableRetryAfter("server_busy", busyRetryAfter)
	}
	return err
}

// busyRetryAfter is the Retry-After sent with 503 server_busy.
const busyRetryAfter = 5 * time.Second

// RateLimitedError is returned when a per-account limit refuses a request.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string { return "auth: rate limited" }

// User is an account as the rest of the server sees it. The password hash
// never leaves this package.
type User struct {
	ID          snowflake.ID
	Username    string
	DisplayName string
	AvatarHash  *string
	CreatedAt   time.Time
	DisabledAt  *time.Time
	// Bio is "about me", possibly empty; it is shown only in the profile.
	Bio string
	// Status is what the user chose: online, idle, dnd or invisible.
	Status string
	// CustomStatus is the user's own status line, or nil.
	CustomStatus *string
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

// SelfUser is the JSON form of a User for that user themself (schema
// CurrentUser in api/openapi.yaml).
type SelfUser struct {
	PublicUser
	Bio          string  `json:"bio"`
	Status       string  `json:"status"`
	CustomStatus *string `json:"custom_status"`
}

// Self returns the JSON form of u for u themself.
func (u User) Self() SelfUser {
	return SelfUser{PublicUser: u.Public(), Bio: u.Bio, Status: u.Status, CustomStatus: u.CustomStatus}
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
	// UsersByIDsIn reads through dbtx (a transaction the caller holds).
	UsersByIDsIn(ctx context.Context, dbtx DB, ids []snowflake.ID) ([]User, error)
	UpdateProfile(ctx context.Context, userID snowflake.ID, displayName string) (User, error)
	// ChangeProfile validates and writes the fields c sets, in one update.
	ChangeProfile(ctx context.Context, userID snowflake.ID, c ProfileChange) (User, error)
	// SetAvatar sets (hash = 16 lower-case hex digits) or clears (nil) the
	// avatar and returns the hash it replaced (nil if there was none).
	SetAvatar(ctx context.Context, userID snowflake.ID, hash *string) (u User, previous *string, err error)
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

	// Failed logins per username, from any IP: 10 per 15 minutes.
	loginFailRate  = rate.Every(15 * time.Minute / 10)
	loginFailBurst = 10
	loginFailTTL   = 30 * time.Minute

	// Failed session lookups per client IP.
	sessionFailRate  = rate.Every(time.Minute / 30)
	sessionFailBurst = 30
	sessionFailTTL   = 2 * time.Minute

	// Logouts per client IP: 30 per minute.
	logoutRate  = rate.Every(time.Minute / 30)
	logoutBurst = 30
	logoutTTL   = 2 * time.Minute
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
	// OnDisabled runs after an account was disabled (voice takes the user
	// out); nil does nothing.
	OnDisabled func(ctx context.Context, userID snowflake.ID)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Per-IP limiters; nil takes the defaults (login 10/min, register 3/h).
	LoginLimiter, RegisterLimiter *ratelimit.Limiter
	// LoginFailureLimiter counts failed logins per username; nil takes the
	// default (10 per 15 min).
	LoginFailureLimiter *ratelimit.Limiter
	// LogoutLimiter limits logouts per client address; nil takes the
	// default (30 per minute).
	LogoutLimiter *ratelimit.Limiter
	// MaxSessions caps the sessions of one user: a new session past it
	// evicts the oldest ones. Zero takes DefaultMaxSessions.
	MaxSessions int
}

// DefaultMaxSessions is how many sessions one user may have; signing in once
// more signs the oldest session out.
const DefaultMaxSessions = 50

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
	onDisabled       func(ctx context.Context, userID snowflake.ID)
	now              func() time.Time
	loginLimiter     *ratelimit.Limiter
	registerLimiter  *ratelimit.Limiter
	loginFailures    *ratelimit.Limiter
	sessionFailures  *ratelimit.Limiter
	sessionLimitLogs *ratelimit.Limiter
	logoutLimiter    *ratelimit.Limiter
	maxSessions      int
	verify           func(ctx context.Context, pw, encoded string) (ok, needsRehash bool, err error)
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
		onDisabled:       o.OnDisabled,
		now:              o.Now,
		loginLimiter:     o.LoginLimiter,
		registerLimiter:  o.RegisterLimiter,
		loginFailures:    o.LoginFailureLimiter,
		logoutLimiter:    o.LogoutLimiter,
		maxSessions:      o.MaxSessions,
		verify:           VerifyPassword,
	}
	for _, name := range o.AdminUsernames {
		s.admins[strings.ToLower(name)] = true
	}
	if s.now == nil {
		s.now = time.Now
	}
	s.sessionFailures = ratelimit.New(sessionFailRate, sessionFailBurst, sessionFailTTL, ratelimit.WithClock(s.now))
	s.sessionLimitLogs = ratelimit.New(rate.Every(time.Minute), 1, sessionFailTTL, ratelimit.WithClock(s.now))
	if s.loginLimiter == nil {
		s.loginLimiter = ratelimit.New(loginRate, loginBurst, loginLimitTTL)
	}
	if s.registerLimiter == nil {
		s.registerLimiter = ratelimit.New(registerRate, registerBurst, registerLimTTL)
	}
	if s.loginFailures == nil {
		s.loginFailures = ratelimit.New(loginFailRate, loginFailBurst, loginFailTTL)
	}
	if s.logoutLimiter == nil {
		s.logoutLimiter = ratelimit.New(logoutRate, logoutBurst, logoutTTL)
	}
	if s.maxSessions <= 0 {
		s.maxSessions = DefaultMaxSessions
	}
	_ = dummyHash() // ready before the first login
	return s
}

func (s *Service) toUser(u store.User) User {
	return User{
		ID:           u.ID,
		Username:     u.Username,
		DisplayName:  u.DisplayName,
		AvatarHash:   u.AvatarHash,
		CreatedAt:    u.CreatedAt,
		DisabledAt:   u.DisabledAt,
		Bio:          u.Bio,
		Status:       u.Status,
		CustomStatus: u.CustomStatus,
		IsAdmin:      s.admins[u.Username],
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

// NormalizeUsername lower-cases a username that has the allowed shape. ASCII
// is checked before lower-casing, so look-alikes such as the Kelvin sign
// cannot fold into a valid name.
func NormalizeUsername(s string) (string, bool) {
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
// ends of s and checks it is 1 to 32 characters, has a visible character and
// contains no control characters and no bidi embedding, override or isolate
// anywhere (see textx).
func NormalizeDisplayName(s string) (string, error) {
	t := strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
	var msg string
	switch n := utf8.RuneCountInString(t); {
	case textx.ContainsBidiControl(s):
		msg = "must not contain bidirectional control characters"
	case n == 0:
		msg = "must not be empty"
	case n > 32:
		msg = "must be at most 32 characters"
	case strings.ContainsFunc(t, unicode.IsControl):
		msg = "must not contain control characters"
	case !textx.HasVisible(t):
		msg = "must contain a visible character"
	default:
		return t, nil
	}
	return "", &ValidationError{Fields: []httpx.FieldError{{Field: "display_name", Message: msg}}}
}

func validateRegistration(in RegisterInput) (validRegistration, error) {
	var v validRegistration
	var fields []httpx.FieldError
	name, ok := NormalizeUsername(in.Username)
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

// CreateAdmin provisions a configured administrator without opening a session.
func (s *Service) CreateAdmin(ctx context.Context, username, password string) (User, error) {
	v, err := validateRegistration(RegisterInput{Username: username, Password: password})
	if err != nil {
		return User{}, err
	}
	if !s.admins[v.username] {
		return User{}, ErrAdminUsernameNotListed
	}
	hash, err := HashPassword(ctx, v.password)
	if err != nil {
		return User{}, err
	}
	row, err := s.createAccount(ctx, s.pool, v, hash, s.now())
	if err != nil {
		return User{}, err
	}
	return s.toUser(row), nil
}

// createAccount inserts a validated user, preserving the unique username on a retry.
func (s *Service) createAccount(ctx context.Context, q store.DBTX, v validRegistration, hash string, now time.Time) (store.User, error) {
	row, err := s.q.CreateUser(ctx, q, store.CreateUserParams{
		ID: s.ids.Next(), Username: v.username, DisplayName: v.displayName,
		PasswordHash: hash, CreatedAt: now,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, ErrUsernameTaken
	}
	if err != nil {
		return store.User{}, fmt.Errorf("auth: create user: %w", err)
	}
	return row, nil
}

// register creates an ordinary account and its first session in one transaction.
func (s *Service) register(ctx context.Context, v validRegistration, meta SessionMeta) (User, string, error) {
	hash, err := HashPassword(ctx, v.password)
	if err != nil {
		return User{}, "", err
	}
	if s.admins[v.username] {
		return User{}, "", ErrUsernameTaken
	}
	now := s.now()
	var (
		row     store.User
		token   string
		evicted []snowflake.ID
	)
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var txErr error
		row, txErr = s.createAccount(ctx, tx, v, hash, now)
		if txErr != nil {
			return txErr
		}
		token, evicted, txErr = s.createSession(ctx, tx, row.ID, meta, now)
		return txErr
	})
	if err != nil {
		return User{}, "", err
	}
	s.publishRevoked(ctx, row.ID, evicted)
	return s.toUser(row), token, nil
}

// Login checks the credentials and opens a session. Unknown users, malformed
// usernames and wrong passwords all return ErrInvalidCredentials after one
// full Argon2id verification, so neither the answer nor its timing tells
// them apart. A disabled account is reported only after a correct password.
//
// Failed attempts are also counted per username, from any IP: past 10 in 15
// minutes further attempts get a RateLimitedError before any hashing, even
// with the right password. A success does not reset the count.
func (s *Service) Login(ctx context.Context, username, password string, meta SessionMeta) (User, string, error) {
	failKey := loginFailureKey(username)
	if ok, retry := s.loginFailures.Check(failKey); !ok {
		return User{}, "", &RateLimitedError{RetryAfter: retry}
	}
	u, token, err := s.login(ctx, username, password, meta)
	if errors.Is(err, ErrInvalidCredentials) {
		s.loginFailures.Charge(failKey)
	}
	return u, token, err
}

// loginFailureKey is the per-username throttle key: the name lower-cased as
// it is stored, cut to a bounded length so junk input cannot bloat the table.
func loginFailureKey(username string) string {
	return truncateUTF8(strings.ToLower(username), 64)
}

func (s *Service) login(ctx context.Context, username, password string, meta SessionMeta) (User, string, error) {
	var row store.User
	found := false
	if name, ok := NormalizeUsername(username); ok {
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
		if _, _, err := s.verify(ctx, password, dummyHash()); err != nil {
			return User{}, "", err // busy or cancelled
		}
		return User{}, "", ErrInvalidCredentials
	}

	ok, rehash, err := s.verify(ctx, password, row.PasswordHash)
	if err != nil {
		return User{}, "", fmt.Errorf("auth: verify password of user %d: %w", row.ID, err)
	}
	if !ok {
		return User{}, "", ErrInvalidCredentials
	}
	if row.DisabledAt != nil {
		return User{}, "", ErrAccountDisabled
	}
	var (
		token   string
		evicted []snowflake.ID
	)
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		current, txErr := s.q.LockUserCredentials(ctx, tx, row.ID)
		if errors.Is(txErr, pgx.ErrNoRows) {
			return ErrInvalidCredentials
		}
		if txErr != nil {
			return fmt.Errorf("auth: lock credentials: %w", txErr)
		}
		if current.DisabledAt != nil || current.PasswordHash != row.PasswordHash {
			return ErrInvalidCredentials
		}
		token, evicted, txErr = s.createSession(ctx, tx, row.ID, meta, s.now())
		row = current
		return txErr
	})
	if err != nil {
		return User{}, "", err
	}
	s.publishRevoked(ctx, row.ID, evicted)
	if rehash {
		s.upgradeHash(ctx, row, password)
	}
	return s.toUser(row), token, nil
}

// upgradeHash re-hashes a password stored with old parameters. Failure only
// postpones the upgrade to the next login.
func (s *Service) upgradeHash(ctx context.Context, verified store.User, password string) {
	hash, err := HashPassword(ctx, password)
	if err == nil {
		_, err = s.q.ReplacePasswordHash(ctx, s.pool, store.ReplacePasswordHashParams{
			ID: verified.ID, PasswordHash: hash, VerifiedHash: verified.PasswordHash,
		})
	}
	if err != nil {
		httpx.LoggerFrom(ctx).WarnContext(ctx, "upgrade password hash", "user_id", verified.ID.String(), "err", err.Error())
	}
}

// createSession opens a session inside tx. Past maxSessions it evicts the
// oldest sessions of the user, returning their IDs for the caller to
// announce once tx has committed. The caller holds the user row lock or
// inserted the user in this transaction.
func (s *Service) createSession(ctx context.Context, tx pgx.Tx, userID snowflake.ID, meta SessionMeta, now time.Time) (string, []snowflake.ID, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", nil, err
	}
	if err = s.q.DeleteExpiredUserSessions(ctx, tx, store.DeleteExpiredUserSessionsParams{UserID: userID, Now: now}); err != nil {
		return "", nil, fmt.Errorf("auth: purge expired sessions: %w", err)
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
		return "", nil, fmt.Errorf("auth: create session: %w", err)
	}
	evicted, err := s.q.DeleteSessionsBeyond(ctx, tx,
		store.DeleteSessionsBeyondParams{UserID: int64(userID), Keep: int64(s.maxSessions)})
	if err != nil {
		return "", nil, fmt.Errorf("auth: evict old sessions: %w", err)
	}
	return token, evicted, nil
}

type session struct {
	id         snowflake.ID
	user       User
	lastUsedAt time.Time
}

// lookup checks a network client's failure budget before reading the database.
func (s *Service) lookup(ctx context.Context, rawToken, clientKey string) (session, error) {
	if !wellFormedToken(rawToken) {
		return session{}, ErrUnauthenticated
	}
	rejected := false
	if clientKey != "" {
		finish, ok := s.sessionFailures.BeginAttempt(clientKey)
		if !ok {
			if log, _ := s.sessionLimitLogs.Allow(clientKey); log {
				httpx.LoggerFrom(ctx).WarnContext(ctx, "session lookup throttled", "client", clientKey)
			}
			return session{}, ErrUnauthenticated
		}
		defer func() { finish(rejected) }()
	}
	row, err := s.q.SessionByTokenHash(ctx, s.pool, store.SessionByTokenHashParams{
		TokenHash: hashToken(rawToken),
		Now:       s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		rejected = true
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
	return s.AuthenticateFrom(ctx, rawToken, "")
}

// AuthenticateFrom applies the shared HTTP and gateway budget for clientKey.
func (s *Service) AuthenticateFrom(ctx context.Context, rawToken, clientKey string) (User, snowflake.ID, error) {
	sess, err := s.lookup(ctx, rawToken, clientKey)
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

// GetUserByUsername returns the account with that username (compared like
// at login: case-insensitively), or ErrUserNotFound, also for a string that
// cannot be a username.
func (s *Service) GetUserByUsername(ctx context.Context, username string) (User, error) {
	name, ok := NormalizeUsername(username)
	if !ok {
		return User{}, ErrUserNotFound
	}
	row, err := s.q.UserByUsername(ctx, s.pool, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: load user: %w", err)
	}
	return s.toUser(row), nil
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
	return s.UsersByIDsIn(ctx, s.pool, ids)
}

// DB is a database handle account reads can run through: the pool or a
// transaction of another module. A caller holding a transaction must pass
// it, never let the read take a second connection from the pool: callers
// that each hold one connection and wait for another wedge the pool.
type DB = store.DBTX

// UsersByIDsIn is UsersByIDs read through dbtx.
func (s *Service) UsersByIDsIn(ctx context.Context, dbtx DB, ids []snowflake.ID) ([]User, error) {
	if len(ids) == 0 {
		return []User{}, nil
	}
	raw := make([]int64, len(ids))
	for i, id := range ids {
		raw[i] = int64(id)
	}
	rows, err := s.q.UsersByIDs(ctx, dbtx, raw)
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

// ProfileChange lists the profile fields to change; nil leaves a field as it
// is. CustomStatus is written only when SetCustomStatus is true, and nil
// then clears it.
type ProfileChange struct {
	DisplayName     *string
	Bio             *string
	Status          *string
	SetCustomStatus bool
	CustomStatus    *string
}

// Statuses a user can choose.
const (
	StatusOnline    = "online"
	StatusIdle      = "idle"
	StatusDND       = "dnd"
	StatusInvisible = "invisible"
)

// ChangeProfile implements Accounts. Every field is checked first; the
// errors of all fields come back together.
func (s *Service) ChangeProfile(ctx context.Context, userID snowflake.ID, c ProfileChange) (User, error) {
	var fields []httpx.FieldError
	arg := store.ChangeProfileParams{ID: userID, SetCustomStatus: c.SetCustomStatus}
	if c.DisplayName != nil {
		dn, err := NormalizeDisplayName(*c.DisplayName)
		var verr *ValidationError
		if errors.As(err, &verr) {
			fields = append(fields, verr.Fields...)
		}
		arg.DisplayName = &dn
	}
	if c.Bio != nil {
		bio, msg := normalizeBio(*c.Bio)
		if msg != "" {
			fields = append(fields, httpx.FieldError{Field: "bio", Message: msg})
		}
		arg.Bio = &bio
	}
	if c.Status != nil {
		switch *c.Status {
		case StatusOnline, StatusIdle, StatusDND, StatusInvisible:
			arg.Status = c.Status
		default:
			fields = append(fields, httpx.FieldError{Field: "status", Message: "must be online, idle, dnd or invisible"})
		}
	}
	if c.SetCustomStatus && c.CustomStatus != nil {
		line, msg := normalizeCustomStatus(*c.CustomStatus)
		if msg != "" {
			fields = append(fields, httpx.FieldError{Field: "custom_status", Message: msg})
		}
		arg.CustomStatus = line
	}
	if len(fields) > 0 {
		return User{}, &ValidationError{Fields: fields}
	}
	row, err := s.q.ChangeProfile(ctx, s.pool, arg)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: change profile: %w", err)
	}
	return s.toUser(row), nil
}

// normalizeBio trims the ends of "about me"; line breaks inside are kept.
func normalizeBio(s string) (string, string) {
	t := strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
	switch {
	case textx.ContainsBidiControl(s):
		return "", "must not contain bidirectional control characters"
	case utf8.RuneCountInString(t) > 190:
		return "", "must be at most 190 characters"
	case strings.ContainsFunc(t, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }):
		return "", "must not contain control characters other than line breaks"
	}
	return t, ""
}

// normalizeCustomStatus trims a status line; one with nothing visible left
// clears the status (nil).
func normalizeCustomStatus(s string) (*string, string) {
	t := strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
	switch {
	case textx.ContainsBidiControl(s):
		return nil, "must not contain bidirectional control characters"
	case utf8.RuneCountInString(t) > 128:
		return nil, "must be at most 128 characters"
	case strings.ContainsFunc(t, unicode.IsControl):
		return nil, "must not contain control characters"
	case !textx.HasVisible(t):
		return nil, ""
	}
	return &t, ""
}

var avatarHashRE = regexp.MustCompile(`^[0-9a-f]{16}$`)

// SetAvatar stores or clears the avatar hash and returns the one it
// replaced. The row is locked first, so of two concurrent calls each sees the
// hash the other one set, and no replaced hash is reported twice or missed.
func (s *Service) SetAvatar(ctx context.Context, userID snowflake.ID, hash *string) (User, *string, error) {
	if hash != nil && !avatarHashRE.MatchString(*hash) {
		return User{}, nil, fmt.Errorf("auth: avatar hash %q is not 16 hex digits", *hash)
	}
	row, err := s.q.ReplaceAvatarHash(ctx, s.pool, store.ReplaceAvatarHashParams{ID: userID, AvatarHash: hash})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, nil, ErrUserNotFound
	}
	if err != nil {
		return User{}, nil, fmt.Errorf("auth: update avatar: %w", err)
	}
	return s.toUser(row.User), row.PreviousHash, nil
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
		if _, _, verr := s.verify(ctx, current, dummyHash()); verr != nil {
			return verr // busy or cancelled
		}
		return ErrInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("auth: load user: %w", err)
	}
	ok, _, err := s.verify(ctx, current, row.PasswordHash)
	if err != nil {
		return fmt.Errorf("auth: verify password of user %d: %w", row.ID, err)
	}
	if !ok {
		return ErrInvalidCredentials
	}

	hash, err := HashPassword(ctx, next)
	if err != nil {
		return err
	}
	var revoked []snowflake.ID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// The conditional UPDATE locks the row and rechecks the verified hash
		// before writing. Argon2 runs outside the transaction.
		n, txErr := s.q.ReplacePasswordHash(ctx, tx, store.ReplacePasswordHashParams{
			ID: userID, PasswordHash: hash, VerifiedHash: row.PasswordHash,
		})
		if txErr != nil {
			return fmt.Errorf("auth: set password: %w", txErr)
		}
		if n == 0 {
			return ErrInvalidCredentials
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

// ResetPassword sets a new password without knowing the old one (an
// administrator's reset) and revokes every session of the user, announcing
// the revocation once the change has committed.
func (s *Service) ResetPassword(ctx context.Context, userID snowflake.ID, next string) error {
	if msg := passwordProblem(next); msg != "" {
		return &ValidationError{Fields: []httpx.FieldError{{Field: "new_password", Message: msg}}}
	}
	if _, err := s.GetUser(ctx, userID); err != nil {
		return err // unknown users cost no hashing
	}
	hash, err := HashPassword(ctx, next)
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
		revoked, txErr = s.q.DeleteUserSessions(ctx, tx, userID)
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

// SetDisabled disables or re-enables an account. Disabling also revokes
// every session of the user (announced after commit); a disabled account
// cannot sign in until it is enabled again. Both are idempotent.
func (s *Service) SetDisabled(ctx context.Context, userID snowflake.ID, disabled bool) (User, error) {
	var row store.User
	var revoked []snowflake.ID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var txErr error
		if !disabled {
			row, txErr = s.q.EnableUser(ctx, tx, userID)
		} else {
			row, txErr = s.q.DisableUser(ctx, tx, store.DisableUserParams{ID: userID, Now: s.now()})
		}
		if errors.Is(txErr, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		if txErr != nil {
			return fmt.Errorf("auth: set disabled: %w", txErr)
		}
		if disabled {
			if revoked, txErr = s.q.DeleteUserSessions(ctx, tx, userID); txErr != nil {
				return fmt.Errorf("auth: revoke sessions: %w", txErr)
			}
		}
		return nil
	})
	if err != nil {
		return User{}, err
	}
	s.publishRevoked(ctx, userID, revoked)
	if disabled && s.onDisabled != nil {
		s.onDisabled(ctx, userID)
	}
	return s.toUser(row), nil
}
