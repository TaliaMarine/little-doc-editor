// Command migrate applies (or rolls back) database migrations. Built as its
// own small binary so the api service's runtime image (distroless, no shell)
// doesn't need the full golang-migrate CLI installed — this uses the same
// library programmatically. Invoked by the Helm chart's pre-install/
// pre-upgrade hook Job (SPEC.md §7.1), not by the api container itself.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	direction := flag.String("direction", "up", "up or down")
	path := flag.String("path", "db/migrations", "path to migration files")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	m, err := migrate.New(fmt.Sprintf("file://%s", *path), dbURL)
	if err != nil {
		slog.Error("failed to initialize migrate", "error", err)
		os.Exit(1)
	}

	switch *direction {
	case "up":
		err = m.Up()
	case "down":
		err = m.Down()
	default:
		slog.Error("direction must be up or down", "direction", *direction)
		os.Exit(1)
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	slog.Info("migration complete", "direction", *direction)
}
