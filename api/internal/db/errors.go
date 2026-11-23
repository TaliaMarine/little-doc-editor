package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Postgres SQLSTATE codes handlers need to tell apart from genuine failures.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// violation, e.g. a duplicate space slug — a client error (409), not a 500.
func IsUniqueViolation(err error) bool {
	return hasSQLState(err, pgUniqueViolation)
}

// IsForeignKeyViolation reports whether err is a Postgres foreign-key
// violation, e.g. adding a space member for a user ID that doesn't exist.
func IsForeignKeyViolation(err error) bool {
	return hasSQLState(err, pgForeignKeyViolation)
}

func hasSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
