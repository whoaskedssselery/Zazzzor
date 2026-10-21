package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/whoaskedssselery/zazzzor/server/internal/admin"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/users"
)

type routerDeps struct {
	Config  config.Config
	Logger  *slog.Logger
	Metrics *httpMetrics
	// Bus carries domain events from the modules to the gateway.
	Bus *events.Bus
	// Modules serve the domain routes; nil leaves them out (tests without a
	// database).
	Modules *modules
	// Ready reports whether the server can take traffic (e.g. the database
	// answers). nil means always ready.
	Ready func(context.Context) error
	// Deadlines bounds body read and response write time per request; zero
	// values take the httpx defaults.
	Deadlines httpx.DeadlineOptions
}

func newRouter(d routerDeps) http.Handler {
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		// A miss inside a mounted subrouter still carries the mount pattern
		// (e.g. "/api/v1/*"); drop it so metrics label the miss "unmatched".
		if rc := chi.RouteContext(r.Context()); rc != nil {
			rc.RoutePatterns = rc.RoutePatterns[:0]
		}
		httpx.WriteProblem(w, httpx.NotFound())
	})
	r.MethodNotAllowed(httpx.Wrap(func(http.ResponseWriter, *http.Request) error { return httpx.MethodNotAllowed() }))

	r.Use(
		// First, so it sees the raw connection writer.
		httpx.Deadlines(d.Deadlines),
		withRequestID,
		withRequestLog(d.Logger, d.Config),
		d.Metrics.middleware,
		withRecover,
		httpx.RequireOrigin(d.Config.AllowedOrigins),
	)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			httpx.JSON(w, http.StatusOK, statusBody{Status: "ok"})
		})
		r.Get("/readyz", httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
			if d.Ready != nil {
				if err := d.Ready(r.Context()); err != nil {
					httpx.LoggerFrom(r.Context()).WarnContext(r.Context(), "not ready", "err", err.Error())
					return httpx.ServiceUnavailable("not_ready")
				}
			}
			httpx.JSON(w, http.StatusOK, statusBody{Status: "ok"})
			return nil
		}))

		if m := d.Modules; m != nil {
			// Public images need no session lookup.
			files.Mount(r, m.Files)
			r.Group(func(r chi.Router) {
				r.Use(auth.Middleware(m.Auth))
				auth.Mount(r, m.Auth)
				users.Mount(r, m.Users)
				admin.Mount(r, m.Admin)
			})
		}
	})
	return r
}

type statusBody struct {
	Status string `json:"status"`
}

type requestIDKey struct{}

// withRequestID gives every request a fresh random ID. Incoming X-Request-Id
// headers are ignored so clients cannot inject values into our logs.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [12]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// withRequestLog stores a request-scoped logger in the context and logs one
// line per request. It logs the route pattern, never the raw path, query,
// headers or body: paths may carry invite codes and headers carry cookies.
func withRequestLog(base *slog.Logger, cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			logger := base.With("request_id", requestID(r.Context()))
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r.WithContext(httpx.WithLogger(r.Context(), logger)))

			logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("method", r.Method),
				slog.String("route", routeLabel(r)),
				slog.Int("status", statusOf(ww)),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				slog.String("client_ip", httpx.ClientIP(r, cfg.TrustedProxies).String()),
			)
		})
	}
}

// withRecover turns a panic into 500 internal and logs it with a stack trace.
func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverPanic(w, r)
		next.ServeHTTP(w, r)
	})
}

// recoverPanic must be deferred directly so that recover() sees the panic.
// http.ErrAbortHandler is re-raised: net/http uses it to abort a response.
func recoverPanic(w http.ResponseWriter, r *http.Request) {
	rec := recover()
	if rec == nil {
		return
	}
	if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
		panic(rec)
	}
	ctx := r.Context()
	httpx.LoggerFrom(ctx).ErrorContext(ctx, "panic", "value", rec, "stack", string(debug.Stack()))
	httpx.WriteProblem(w, httpx.Internal())
}

type httpMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func newHTTPMetrics(reg prometheus.Registerer) *httpMetrics {
	m := &httpMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "zazzzor_http_requests_total",
			Help: "HTTP requests by route pattern, method and status.",
		}, []string{"route", "method", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "zazzzor_http_request_duration_seconds",
			Help:    "HTTP request latency by route pattern and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),
	}
	reg.MustRegister(m.requests, m.duration)
	return m
}

func (m *httpMetrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		route, method := routeLabel(r), methodLabel(r.Method)
		m.requests.WithLabelValues(route, method, strconv.Itoa(statusOf(ww))).Inc()
		m.duration.WithLabelValues(route, method).Observe(time.Since(start).Seconds())
	})
}

// routeLabel is the matched chi pattern, or "unmatched", keeping label
// cardinality bounded.
func routeLabel(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

func methodLabel(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return m
	default:
		return "OTHER"
	}
}

func statusOf(ww middleware.WrapResponseWriter) int {
	if s := ww.Status(); s != 0 {
		return s
	}
	return http.StatusOK
}
