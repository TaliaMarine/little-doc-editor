// Command api runs the little-doc-editor REST API server.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authz"
	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/oidcapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/passwordhash"
	"github.com/TaliaMarine/little-doc-editor/api/internal/server"
	"github.com/TaliaMarine/little-doc-editor/api/internal/storage"
)

// devAdminUserID must match authctx's localDevAdminUserID — kept as a
// separate constant here (rather than exported from authctx) so authctx
// doesn't need to depend on the db package just to seed a row.
var devAdminUserID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// devAdminEmail/devAdminPassword log the frontend in as the bypass identity
// (see DEVELOPER.md). The row is seeded (and enabled) only when
// DEV_AUTH_BYPASS is on; on every startup with it off, the row is disabled
// instead — otherwise a database that ever ran with the bypass, even once,
// would keep an org-admin account with a published password.
const (
	devAdminEmail    = "dev-admin@localhost"
	devAdminPassword = "dev"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := db.New(pool)

	if cfg.DevAuthBypass {
		if err := seedDevAdminUser(ctx, queries); err != nil {
			return err
		}
	} else if err := queries.SetUserDisabled(ctx, db.SetUserDisabledParams{
		ID:       db.ToPgUUID(devAdminUserID),
		Disabled: true,
	}); err != nil { // a no-op if the row was never seeded
		return err
	}
	if cfg.BootstrapAdminEmail != "" {
		if err := bootstrapAdmin(ctx, queries, cfg, logger); err != nil {
			return err
		}
	}

	authz.SetDefaultMemberRole(cfg.DefaultMemberRole)

	store, err := storage.Open(ctx, cfg)
	if err != nil {
		return err
	}

	oidcHandler, err := oidcapi.NewHandler(ctx, queries, cfg, logger)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.New(logger, pool, store, oidcHandler, cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// seedDevAdminUser gives the fixed dev-auth-bypass identity a real row in
// the users table, so foreign-key-bearing inserts (pages, attachments, ...)
// have something valid to reference, and a real login for the frontend.
// Idempotent — safe to run on every startup.
func seedDevAdminUser(ctx context.Context, queries *db.Queries) error {
	passwordHash, err := passwordhash.Hash(devAdminPassword)
	if err != nil {
		return err
	}
	_, err = queries.UpsertDevAdminUser(ctx, db.UpsertDevAdminUserParams{
		ID:           db.ToPgUUID(devAdminUserID),
		Email:        devAdminEmail,
		DisplayName:  "Local Dev Admin",
		PasswordHash: &passwordHash,
	})
	return err
}

// bootstrapAdmin creates the first internal org-admin account from
// BOOTSTRAP_ADMIN_EMAIL/PASSWORD if no org admin exists yet (Decision 11:
// admin-invite-only signup — someone has to send the first invite). A no-op
// once any org admin exists, so it's safe to leave these env vars set
// across restarts.
func bootstrapAdmin(ctx context.Context, queries *db.Queries, cfg config.Config, logger *slog.Logger) error {
	count, err := queries.CountOrgAdmins(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hash, err := passwordhash.Hash(cfg.BootstrapAdminPassword)
	if err != nil {
		return err
	}
	if _, err := queries.CreateInternalOrgAdmin(ctx, db.CreateInternalOrgAdminParams{
		Email:        cfg.BootstrapAdminEmail,
		DisplayName:  "Admin",
		PasswordHash: &hash,
	}); err != nil {
		return err
	}

	logger.Warn("bootstrap admin account created", "email", cfg.BootstrapAdminEmail)
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
