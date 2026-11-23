// Package sessionrecord provides the small helpers authapi and shareapi
// both need to write a `sessions` row alongside issuing a session token —
// kept separate from internal/sessiontoken since that package only deals
// with the JWT itself, not persistence.
package sessionrecord

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// HashToken returns the value stored in sessions.token_hash — never the
// token itself, so a database read alone can't be replayed as a session.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func ExpiresAt(ttl time.Duration) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(ttl), Valid: true}
}
