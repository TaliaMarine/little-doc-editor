// Package server wires up the chi router and HTTP handlers for the api service.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TaliaMarine/little-doc-editor/api/internal/accountsapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/adminapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/attachments"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/clientip"
	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/cors"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/exportapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/oidcapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/pagesapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/pagesync"
	"github.com/TaliaMarine/little-doc-editor/api/internal/ratelimit"
	"github.com/TaliaMarine/little-doc-editor/api/internal/revisions"
	"github.com/TaliaMarine/little-doc-editor/api/internal/searchapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/searchindex"
	"github.com/TaliaMarine/little-doc-editor/api/internal/shareapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/spacesapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/storage"
	"github.com/TaliaMarine/little-doc-editor/api/internal/telemetry"
)

// New builds the chi router with all api routes registered. oidcHandler is
// constructed by the caller (see cmd/api/main.go) since building it can
// fail — OIDC issuer discovery happens at construction time, and a
// misconfigured issuer should fail api startup loudly, not get silently
// swallowed inside route setup.
func New(logger *slog.Logger, pool *pgxpool.Pool, store storage.Storage, oidcHandler *oidcapi.Handler, cfg config.Config) http.Handler {
	r := chi.NewRouter()
	queries := db.New(pool)
	indexer := searchindex.NewIndexer(queries)
	querier := searchindex.NewQuerier(queries)

	ips, _ := clientip.New(cfg.TrustedProxies) // validated by config.Load
	limiter := ratelimit.New(cfg.RateLimitEnabled, ratelimit.RedisOptions{Addr: cfg.RedisAddr, Username: cfg.RedisUsername, Password: cfg.RedisPassword, TLS: cfg.RedisTLS, DB: cfg.RedisDB}, ips, logger)
	recorder := revisions.NewRecorder(cfg.RevisionDebounce)
	// A nil *Notifier (no REDIS_ADDR) is a valid no-op; the typed nil must
	// not reach the interface, hence the explicit branch.
	var contentNotifier pagesapi.ContentNotifier
	// (Redis pub/sub is database-independent, so cfg.RedisDB isn't passed here.)
	if n := pagesync.New(pagesync.Options{Addr: cfg.RedisAddr, Username: cfg.RedisUsername, Password: cfg.RedisPassword, TLS: cfg.RedisTLS}); n != nil {
		contentNotifier = n
	} else {
		logger.Info("REDIS_ADDR unset: collab is not notified of restores/content PUTs (applied at its next store)")
	}

	authHandler := authapi.NewHandler(queries, cfg, logger, limiter)
	shareHandler := shareapi.NewHandler(queries, cfg, logger, ips, limiter)
	accountsHandler := accountsapi.NewHandler(pool, queries, cfg, logger)

	r.Use(telemetry.Middleware(redactPath))
	// Opt-in (CORS_ALLOWED_ORIGINS); ahead of routing/auth/rate limiting, so
	// preflights never need a session and 401/429 responses still carry the
	// headers the browser needs to read them.
	r.Use(cors.Middleware(cfg.CORSAllowedOrigins))
	r.Use(middleware.RequestID)
	r.Use(slogRequestLogger(logger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", handleHealthz())
	r.Get("/readyz", handleReadyz(pool))

	r.Route("/api/v1", func(r chi.Router) {
		// No session required — these are how one is obtained. Each class is
		// rate limited per client IP under its own counter (SPEC.md §8):
		// every login/redeem costs an argon2id hash or writes rows.
		perMinute := time.Minute
		r.Group(func(r chi.Router) {
			r.Use(limiter.Middleware("login", cfg.RateLimitLoginPerMinute, perMinute))
			authHandler.PublicRoutes(r)
		})
		r.Group(func(r chi.Router) {
			r.Use(limiter.Middleware("share", cfg.RateLimitPublicPerMinute, perMinute))
			shareHandler.PublicRoutes(r)
		})
		r.Group(func(r chi.Router) {
			r.Use(limiter.Middleware("invite", cfg.RateLimitPublicPerMinute, perMinute))
			accountsHandler.PublicRoutes(r)
		})
		r.Group(func(r chi.Router) {
			r.Use(limiter.Middleware("oidc", cfg.RateLimitPublicPerMinute, perMinute))
			oidcHandler.PublicRoutes(r)
		})

		r.Group(func(r chi.Router) {
			r.Use(authctx.Middleware(cfg.DevAuthBypass, cfg.SessionJWTSecret, queries, logger))

			authHandler.PrivateRoutes(r)
			shareHandler.PrivateRoutes(r)
			accountsHandler.PrivateRoutes(r)
			spacesapi.NewHandler(pool, queries, store, logger).Routes(r)
			pagesapi.NewHandler(pool, queries, indexer, recorder, store, contentNotifier, logger).Routes(r)
			attachments.NewHandler(queries, store, cfg, logger).Routes(r)
			exportapi.NewHandler(queries, store, logger).Routes(r)
			searchapi.NewHandler(queries, querier, logger).Routes(r)
			adminapi.NewHandler(queries, cfg, logger).Routes(r)
		})
	})

	return r
}

func handleHealthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleReadyz reports readiness by checking the database connection, since a
// process that's up but can't reach Postgres shouldn't receive traffic.
func handleReadyz(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"error":  err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// redactPath keeps share-link tokens out of request logs: the token in
// /api/v1/share/{token} is a bearer credential (anyone holding a
// password-less link can redeem it), so log the route pattern instead.
func redactPath(r *http.Request) string {
	for _, prefix := range []string{"/api/v1/share/", "/api/v1/invites/"} {
		if strings.HasPrefix(r.URL.Path, prefix) && r.URL.Path != "/api/v1/invites/" {
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				return rc.RoutePattern()
			}
			return prefix + "[redacted]"
		}
	}
	return r.URL.Path
}

// slogRequestLogger logs each request via slog instead of chi's default
// stdlib-log-based middleware.Logger, to keep logging structured throughout.
func slogRequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http_request",
				"method", r.Method,
				"path", redactPath(r),
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
