// Package app wires the domain modules, their routes and the gateway
// together. The command uses it, and so do the integration tests that need
// the production wiring (the gateway's).
package app

import (
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/admin"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/invites"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/users"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// Reads (GET) per signed-in user: 120 per minute, burst 60.
var (
	readRate  = rate.Every(time.Minute / 120)
	readBurst = 60
	readTTL   = 2 * time.Minute
)

// Modules are the domain modules served by the API.
type Modules struct {
	Auth     *auth.Service
	Users    *users.Service
	Guilds   *guilds.Service
	Channels *channels.Service
	Invites  *invites.Service
	Messages *messages.Service
	Admin    *admin.Service
	Files    *files.Disk
	// Voice is unavailable (every operation 503) without LiveKit
	// credentials; its Run must be started by the caller.
	Voice *voice.Service
	// ReadLimiter limits the GETs of each signed-in user (auth.LimitReads);
	// NewModules sets the default, 120 per minute with a burst of 60.
	ReadLimiter *ratelimit.Limiter
}

// NewModules wires the domain modules to the configuration and to each
// other. Images live under cfg.DataDir, which is created if missing. Voice
// talks to LiveKit at cfg.LiveKitURL when its credentials are set and logs
// its background failures to slog.Default().
func NewModules(cfg config.Config, pool *pgxpool.Pool, ids *snowflake.Generator, pub events.Publisher) (*Modules, error) {
	disk, err := files.NewDisk(cfg.DataDir, files.Options{Retention: cfg.FileRetention})
	if err != nil {
		return nil, err
	}
	// auth asks the admin settings on every registration.
	settings := admin.NewSettings(pool)
	authSvc := auth.NewService(auth.Options{
		Pool:             pool,
		IDs:              ids,
		Publisher:        pub,
		AdminUsernames:   cfg.AdminUsernames,
		CookieSecure:     cfg.CookieSecure,
		TrustedProxies:   cfg.TrustedProxies,
		RegistrationOpen: settings.RegistrationOpen,
	})
	// Removals from guilds and channels take people out of voice; the
	// voice service is plugged in below, before anything publishes.
	cleanup := &voiceCleanup{next: pub}
	// guilds runs the channels bootstrapper inside guild creation; channels
	// reads guilds through its exported reads, invites both; messages asks
	// channels what a member may do.
	guildSvc := guilds.NewService(guilds.Options{Pool: pool, IDs: ids, Users: authSvc, Files: disk, Publisher: cleanup,
		Hooks: guilds.Hooks{Channels: channels.NewBootstrapper(ids, nil), Overwrites: channels.NewCleaner()}})
	channelSvc := channels.NewService(channels.Options{Pool: pool, IDs: ids, Guilds: guildSvc, Publisher: cleanup})
	messageSvc := messages.NewService(messages.Options{Pool: pool, IDs: ids, Channels: channelSvc, Users: authSvc,
		Publisher: pub})
	vo := voice.Options{Channels: channelSvc, Publisher: pub, Logger: slog.Default()}
	if cfg.VoiceEnabled() {
		vo.Rooms = voice.NewLiveKit(cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)
		vo.APIKey, vo.APISecret = cfg.LiveKitAPIKey, cfg.LiveKitAPISecret
	}
	cleanup.voice = voice.NewService(vo)
	return &Modules{
		Auth:        authSvc,
		Users:       users.NewService(users.Options{Accounts: authSvc, Files: disk, Publisher: pub}),
		Guilds:      guildSvc,
		Channels:    channelSvc,
		Invites:     invites.NewService(invites.Options{Pool: pool, Channels: channelSvc, Guilds: guildSvc, Users: authSvc}),
		Messages:    messageSvc,
		Admin:       admin.NewService(admin.Options{Settings: settings, Accounts: authSvc}),
		Files:       disk,
		Voice:       cleanup.voice,
		ReadLimiter: ratelimit.New(readRate, readBurst, readTTL),
	}, nil
}

// NewGateway starts the gateway over the modules and bus. opts carries the
// tuning (timeouts, buffers, limits); its dependency fields are set here,
// except Guilds and Channels when the caller set them (tests wrap them).
func (m *Modules) NewGateway(bus *events.Bus, cfg config.Config, logger *slog.Logger, reg prometheus.Registerer,
	opts gateway.Options,
) *gateway.Gateway {
	opts.Auth = m.Auth
	if opts.Guilds == nil {
		opts.Guilds = m.Guilds
	}
	if opts.Channels == nil {
		opts.Channels = m.Channels
	}
	opts.Voice = m.Voice
	opts.Bus = bus
	opts.AllowedOrigins = cfg.AllowedOrigins
	opts.TrustedProxies = cfg.TrustedProxies
	opts.Logger = logger
	opts.Registerer = reg
	return gateway.New(opts)
}

// Routes mounts the domain routes, and the gateway at /gateway unless gw is
// nil, on r (the /api/v1 router). The gateway clears HTTP deadlines only
// after upgrading, and it authenticates by itself; the LiveKit
// webhook has no session but a signature.
func (m *Modules) Routes(r chi.Router, gw *gateway.Gateway) {
	// Public images need no session lookup.
	files.Mount(r, m.Files)
	voice.MountWebhook(r, m.Voice)
	if gw != nil {
		r.With(httpx.WithoutDeadlines).Get("/gateway", gw.ServeHTTP)
	}
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(m.Auth), auth.LimitReads(m.ReadLimiter))
		auth.Mount(r, m.Auth)
		users.Mount(r, m.Users)
		admin.Mount(r, m.Admin)
		guilds.Mount(r, m.Guilds)
		channels.Mount(r, m.Channels)
		invites.Mount(r, m.Invites)
		messages.Mount(r, m.Messages)
		voice.Mount(r, m.Voice)
	})
}
