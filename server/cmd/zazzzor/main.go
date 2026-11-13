// Command zazzzor is the Zazzzor API server.
//
// Usage:
//
//	zazzzor                              run the server (configured through ZAZZZOR_* env)
//	zazzzor --version                    print the version
//	zazzzor healthcheck                  exit 0 if GET /api/v1/readyz on the local port is 200
//	zazzzor smoke <url>                  end-to-end check of a running API (see smoke.go)
//	zazzzor files reconcile              make the images match the database (see files.go)
//	zazzzor schema-version               print the schema version this binary migrates to
//	zazzzor create-admin <username>      create a configured admin with a password from stdin
//	zazzzor restore-check seed <url>     create data to check a restore with (see restorecheck.go)
//	zazzzor restore-check verify <url>   check that data after the restore
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

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/whoaskedssselery/zazzzor/server/internal/app"
	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	shutdownTimeout = 15 * time.Second
	// gatewayShutdownTimeout bounds the wait for gateway connections to
	// finish their close handshake.
	gatewayShutdownTimeout = 5 * time.Second
	healthcheckTimeout     = 3 * time.Second
	dbAttemptTimeout       = 5 * time.Second
	migrateTimeout         = 60 * time.Second
	maxDBRetryDelay        = 5 * time.Second
	readyTimeout           = 2 * time.Second
	readHeaderTimeout      = 10 * time.Second
	idleTimeout            = 120 * time.Second
)

// startupDBTimeout bounds the wait for the database to come up at start-up
// (Docker Swarm starts services in no particular order); tests shorten it.
var startupDBTimeout = 60 * time.Second

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
		case "smoke":
			return smokeCommand(args[1:], env, stdout, stderr)
		case "files":
			return filesCommand(args[1:], env, stdout, stderr)
		case "schema-version":
			return schemaVersionCommand(stdout, stderr)
		case "create-admin":
			return createAdminCommand(args[1:], env, os.Stdin, stdout, stderr)
		case "restore-check":
			return restoreCheckCommand(args[1:], env, os.Stdin, stdout, stderr)
		default:
			_, _ = fmt.Fprintf(stderr, "unknown command %q; usage: zazzzor [--version | healthcheck | smoke <base_url> | "+
				"files reconcile | schema-version | create-admin <username> | restore-check seed|verify <base_url>]\n", args[0])
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

	// The schema must be current before any request is served. A database
	// that is still starting is waited for (Docker Swarm has no depends_on);
	// one that stays down, or a migration that fails, stops the start-up.
	pool, err := openDatabase(ctx, logger, cfg.DatabaseURL)
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
		m, err := app.NewModules(cfg, pool, snowflake.NewGenerator(0, time.Now), bus)
		if err != nil {
			return err
		}
		if err = reg.Register(m.Voice.Metrics()); err != nil {
			return fmt.Errorf("zazzzor: register voice metrics: %w", err)
		}
		deps.Ready = dbReady(pool)
		deps.Modules = m
		deps.Gateway = m.NewGateway(bus, cfg, logger, reg, gateway.Options{})
	}
	// Trash is purged while the API serves; shutdown waits for the job.
	if deps.Modules != nil {
		stopPreviews := startBackground(ctx, deps.Modules.Messages.RunPreviews)
		defer stopPreviews()
		stopAttachments := startBackground(ctx, deps.Modules.Messages.RunAttachmentSweep)
		defer stopAttachments()
		stopAudit := startBackground(ctx, func(ctx context.Context) { deps.Modules.Audit.RunPrune(ctx, logger) })
		defer stopAudit()
		stopPurge := startFilePurge(ctx, deps.Modules.Files, logger)
		defer stopPurge()
		stopVoice := startVoice(ctx, deps.Modules.Voice, logger)
		defer stopVoice()
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
	// Hijacked WebSockets are invisible to http.Server.Shutdown: the
	// gateway closes them itself (1012), within a bounded wait.
	if deps.Gateway != nil {
		gwCtx, gwCancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayShutdownTimeout)
		if err := deps.Gateway.Close(gwCtx); err != nil {
			logger.Warn("gateway connections still open at shutdown", "err", err.Error())
		}
		gwCancel()
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	return errors.Join(runErr, api.Shutdown(shutdownCtx), metrics.Shutdown(shutdownCtx))
}

// startVoice runs the background work of voice (reconciling with LiveKit,
// removing participants) while the API serves, or logs once that voice is
// off. The returned function stops the work and waits for it.
func startVoice(ctx context.Context, v *voice.Service, logger *slog.Logger) (stop func()) {
	if !v.Enabled() {
		logger.Warn("voice is off: set LIVEKIT_API_KEY and LIVEKIT_API_SECRET to enable it")
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		v.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
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

// openDatabase connects to the database, waiting up to startupDBTimeout for
// it to come up, and applies pending migrations.
func openDatabase(ctx context.Context, logger *slog.Logger, url string) (*pgxpool.Pool, error) {
	waitCtx, cancel := context.WithTimeout(ctx, startupDBTimeout)
	defer cancel()
	pool, err := retryOpen(waitCtx, logger, func(ctx context.Context) (*pgxpool.Pool, error) {
		ctx, cancel := context.WithTimeout(ctx, dbAttemptTimeout)
		defer cancel()
		return db.Open(ctx, url)
	})
	if err != nil {
		return nil, err
	}
	// Migrations get their own budget, whatever the wait above used up.
	migrateCtx, cancelMigrate := context.WithTimeout(ctx, migrateTimeout)
	defer cancelMigrate()
	if err := db.Migrate(migrateCtx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// dbRetryDelay is the pause before attempt n+1; tests shorten it.
var dbRetryDelay = retryDelay

// retryDelay doubles from 500 ms up to maxDBRetryDelay.
func retryDelay(attempt int) time.Duration {
	d := 500 * time.Millisecond << min(attempt-1, 4)
	return min(d, maxDBRetryDelay)
}

// retryOpen calls open until it succeeds, fails for good (permanentDBError)
// or ctx ends. In the last case it returns the last error that was not caused
// by ctx ending, so the log names the real cause. db.Open errors never
// contain the URL, so they are safe to log.
func retryOpen[T any](ctx context.Context, logger *slog.Logger, open func(context.Context) (T, error)) (T, error) {
	var zero T
	var cause error
	for attempt := 1; ; attempt++ {
		v, err := open(ctx)
		if err == nil {
			return v, nil
		}
		if permanentDBError(err) {
			return zero, err
		}
		if ctx.Err() == nil || cause == nil {
			cause = err
		}
		if ctx.Err() != nil {
			return zero, fmt.Errorf("database not ready after %s: %w", startupDBTimeout, cause)
		}
		delay := dbRetryDelay(attempt)
		logger.Warn("database not ready, retrying", "attempt", attempt,
			"retry_in", delay.String(), "err", err.Error())
		select {
		case <-ctx.Done():
			return zero, fmt.Errorf("database not ready after %s: %w", startupDBTimeout, cause)
		case <-time.After(delay):
		}
	}
}

// permanentDBError reports errors that waiting cannot fix: a malformed URL,
// a wrong user or password, or a database that does not exist.
func permanentDBError(err error) bool {
	if errors.Is(err, db.ErrInvalidURL) {
		return true
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "28P01", "28000", "3D000": // invalid_password, invalid_authorization_specification, invalid_catalog_name
			return true
		}
	}
	return false
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
