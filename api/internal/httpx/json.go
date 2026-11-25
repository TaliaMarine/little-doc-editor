// Package httpx holds small HTTP response helpers shared across the api
// service's handler packages, to avoid every package redefining its own
// writeJSON/writeError.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// PtrOrNil returns nil for "", and a pointer to s otherwise — for sqlc's
// pgx/v5 codegen (emit_pointers_for_null_types), nullable text/int columns
// are plain Go pointers, with nil meaning SQL NULL. Appropriate for optional
// fields like spaces.icon where an empty string and "not set" should mean
// the same thing.
func PtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// WriteLookupError reports a failed single-row lookup: a missing row
// (pgx.ErrNoRows) is a 404 with notFoundMsg; anything else is a real failure
// — logged and returned as a generic 500 so database outages are not
// disguised as "not found".
func WriteLookupError(w http.ResponseWriter, logger *slog.Logger, err error, notFoundMsg string) {
	if errors.Is(err, pgx.ErrNoRows) {
		WriteError(w, http.StatusNotFound, notFoundMsg)
		return
	}
	logger.Error("database lookup failed", "error", err, "lookup", notFoundMsg)
	WriteError(w, http.StatusInternalServerError, "internal error")
}

// Optional distinguishes a JSON field that was absent from one explicitly
// set to null (or a value): PATCH bodies use it so "leave unchanged" and
// "clear" are different requests.
type Optional[T any] struct {
	Set   bool
	Value *T // nil when the field was explicitly null
}

func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// EscapeLike makes user input literal inside LIKE/ILIKE (backslash is the
// default escape character), so "%" or "_" can't turn a search into a
// match-everything directory dump.
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
