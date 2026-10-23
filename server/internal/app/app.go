// Package app wires the domain modules, their routes and the gateway
// together. The command uses it, and so do the integration tests that need
// the production wiring (the gateway's).
package app

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

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
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/users"
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
}

// NewModules wires the domain modules to the configuration and to each
// other. Images live under cfg.DataDir, which is created if missing.
func NewModules(cfg config.Config, pool *pgxpool.Pool, ids *snowflake.Generator, pub events.Publisher) (*Modules, error) {
	disk, err := files.NewDisk(cfg.DataDir)
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
	// guilds runs the channels bootstrapper inside guild creation; channels
	// reads guilds through its exported reads, invites both; messages asks
	// channels what a member may do.
	guildSvc := guilds.NewService(guilds.Options{Pool: pool, IDs: ids, Users: authSvc, Files: disk, Publisher: pub,
		Hooks: guilds.Hooks{Channels: channels.NewBootstrapper(ids, nil), Overwrites: channels.NewCleaner()}})
	channelSvc := channels.NewService(channels.Options{Pool: pool, IDs: ids, Guilds: guildSvc, Publisher: pub})
	messageSvc := messages.NewService(messages.Options{Pool: pool, IDs: ids, Channels: channelSvc, Users: authSvc,
		Publisher: pub})
	return &Modules{
		Auth:     authSvc,
		Users:    users.NewService(users.Options{Accounts: authSvc, Files: disk, Publisher: pub}),
		Guilds:   guildSvc,
		Channels: channelSvc,
		Invites:  invites.NewService(invites.Options{Pool: pool, Channels: channelSvc, Guilds: guildSvc, Users: authSvc}),
		Messages: messageSvc,
		Admin:    admin.NewService(settings, authSvc),
		Files:    disk,
	}, nil
}

// NewGateway starts the gateway over the modules and bus. opts carries the
// tuning (timeouts, buffers); its dependency fields are set here.
func (m *Modules) NewGateway(bus *events.Bus, cfg config.Config, logger *slog.Logger, reg prometheus.Registerer,
	opts gateway.Options,
) *gateway.Gateway {
	opts.Auth = m.Auth
	opts.Guilds = m.Guilds
	opts.Channels = m.Channels
	opts.Bus = bus
	opts.AllowedOrigins = cfg.AllowedOrigins
	opts.TrustedProxies = cfg.TrustedProxies
	opts.Logger = logger
	opts.Registerer = reg
	return gateway.New(opts)
}

// Routes mounts the domain routes, and the gateway at /gateway unless gw is
// nil, on r (the /api/v1 router). The gateway is the only route without
// the per-request deadlines, and it authenticates by itself.
func (m *Modules) Routes(r chi.Router, gw *gateway.Gateway) {
	// Public images need no session lookup.
	files.Mount(r, m.Files)
	if gw != nil {
		r.With(httpx.WithoutDeadlines).Get("/gateway", gw.ServeHTTP)
	}
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(m.Auth))
		auth.Mount(r, m.Auth)
		users.Mount(r, m.Users)
		admin.Mount(r, m.Admin)
		guilds.Mount(r, m.Guilds)
		channels.Mount(r, m.Channels)
		invites.Mount(r, m.Invites)
		messages.Mount(r, m.Messages)
	})
}
