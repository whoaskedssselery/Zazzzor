// Command zazzzor is the Zazzzor API server.
//
// Usage:
//
//	zazzzor              run the server (configured through ZAZZZOR_* env)
//	zazzzor --version    print the version
//	zazzzor healthcheck  exit 0 if GET /api/v1/readyz on the local port is 200
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/whoaskedssselery/zazzzor/server/internal/admin"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/invites"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/users"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	shutdownTimeout    = 15 * time.Second
	healthcheckTimeout = 3 * time.Second
	startupDBTimeout   = 30 * time.Second
	readyTimeout       = 2 * time.Second
	readHeaderTimeout  = 10 * time.Second
	idleTimeout        = 120 * time.Second
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, env func(string) string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-version", "version":
			_, _ = fmt.Fprintln(stdout, version)
			return 0
		case "healthcheck":
			return healthcheck(env, stderr)
		default:
			_, _ = fmt.Fprintf(stderr, "unknown command %q; usage: zazzzor [--version | healthcheck]\n", args[0])
			return 2
		}
	}

	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load(env)
	if err != nil {
		logger.Error("load config", "err", err.Error())
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The schema must be current before any request is served; a database
	// that is down or a migration that fails stops the start-up.
	pool, err := openDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "err", err.Error())
		return 1
	}
	defer pool.Close()

	apiLn, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("listen api", "addr", cfg.HTTPAddr, "err", err.Error())
		return 1
	}
	metricsLn, err := net.Listen("tcp", cfg.MetricsAddr)
	if err != nil {
		_ = apiLn.Close()
		logger.Error("listen metrics", "addr", cfg.MetricsAddr, "err", err.Error())
		return 1
	}

	logger.Info("starting", "version", version, "http_addr", apiLn.Addr().String(),
		"metrics_addr", metricsLn.Addr().String())
	if err := serve(ctx, logger, cfg, pool, apiLn, metricsLn); err != nil {
		logger.Error("server stopped with error", "err", err.Error())
		return 1
	}
	logger.Info("stopped")
	return 0
}

// serve runs the API and metrics servers on the given listeners until ctx is
// done, then shuts both down gracefully within shutdownTimeout. A nil pool
// serves only the probes (always ready).
func serve(ctx context.Context, logger *slog.Logger, cfg config.Config, pool *pgxpool.Pool,
	apiLn, metricsLn net.Listener,
) error {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	bus := events.NewBus(reg)
	defer bus.Close()

	deps := routerDeps{
		Config:  cfg,
		Logger:  logger,
		Metrics: newHTTPMetrics(reg),
		Bus:     bus,
	}
	if pool != nil {
		m, err := newModules(cfg, pool, snowflake.NewGenerator(0, time.Now), bus)
		if err != nil {
			return err
		}
		deps.Ready = dbReady(pool)
		deps.Modules = m
	}
	api := newServer(newRouter(deps), logger)

	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	metrics := newServer(metricsMux, logger)

	errc := make(chan error, 2)
	go func() { errc <- serveOn(api, apiLn) }()
	go func() { errc <- serveOn(metrics, metricsLn) }()

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errc:
	}

	logger.Info("shutting down", "timeout", shutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	return errors.Join(runErr, api.Shutdown(shutdownCtx), metrics.Shutdown(shutdownCtx))
}

func newServer(h http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    1 << 16,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}

func serveOn(s *http.Server, ln net.Listener) error {
	if err := s.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// openDatabase connects to the database and applies pending migrations.
func openDatabase(ctx context.Context, url string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, startupDBTimeout)
	defer cancel()
	pool, err := db.Open(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// modules are the domain modules served by the API.
type modules struct {
	Auth     *auth.Service
	Users    *users.Service
	Guilds   *guilds.Service
	Channels *channels.Service
	Invites  *invites.Service
	Admin    *admin.Service
	Files    *files.Disk
}

// newModules wires the domain modules to the configuration and to each
// other. Images live under cfg.DataDir, which is created if missing.
func newModules(cfg config.Config, pool *pgxpool.Pool, ids *snowflake.Generator, pub events.Publisher) (*modules, error) {
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
	// reads guilds through its exported reads, invites both.
	guildSvc := guilds.NewService(guilds.Options{Pool: pool, IDs: ids, Users: authSvc, Files: disk, Publisher: pub,
		Hooks: guilds.Hooks{Channels: channels.NewBootstrapper(ids, nil), Overwrites: channels.NewCleaner()}})
	channelSvc := channels.NewService(channels.Options{Pool: pool, IDs: ids, Guilds: guildSvc, Publisher: pub})
	return &modules{
		Auth:     authSvc,
		Users:    users.NewService(users.Options{Accounts: authSvc, Files: disk, Publisher: pub}),
		Guilds:   guildSvc,
		Channels: channelSvc,
		Invites:  invites.NewService(invites.Options{Pool: pool, Channels: channelSvc, Guilds: guildSvc, Users: authSvc}),
		Admin:    admin.NewService(settings, authSvc),
		Files:    disk,
	}, nil
}

// pinger is the part of a pool readiness needs.
type pinger interface {
	Ping(ctx context.Context) error
}

// dbReady reports ready when the database answers a ping within readyTimeout.
func dbReady(p pinger) func(context.Context) error {
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, readyTimeout)
		defer cancel()
		return p.Ping(ctx)
	}
}

// healthcheck probes the local readiness endpoint. It only needs
// ZAZZZOR_HTTP_ADDR, so it works inside a distroless image without the full
// configuration.
func healthcheck(env func(string) string, stderr io.Writer) int {
	addr := env("ZAZZZOR_HTTP_ADDR")
	if addr == "" {
		addr = config.DefaultHTTPAddr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		_, _ = fmt.Fprintln(stderr, "healthcheck: ZAZZZOR_HTTP_ADDR must be host:port")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()
	url := "http://" + net.JoinHostPort("127.0.0.1", port) + "/api/v1/readyz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "healthcheck:", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "healthcheck:", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintln(stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}
