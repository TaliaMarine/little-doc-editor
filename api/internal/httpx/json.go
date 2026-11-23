// Package httpx holds small HTTP response helpers shared across the api
// service's handler packages, to avoid every package redefining its own
// writeJSON/writeError.
package httpx

import (
	"encoding/json"
	"net/http"
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
