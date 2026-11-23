package db

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ToPgUUID and FromUUID convert between google/uuid.UUID (used everywhere
// outside the sqlc-generated query layer, since it has real parsing/
// generation helpers) and pgtype.UUID (what sqlc's pgx/v5 driver expects at
// the query boundary).
func ToPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func FromPgUUID(id pgtype.UUID) uuid.UUID {
	return uuid.UUID(id.Bytes)
}
