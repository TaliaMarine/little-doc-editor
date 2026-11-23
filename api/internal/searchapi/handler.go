// Package searchapi exposes SPEC.md §6.8's search over a space's pages.
package searchapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authz"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/searchindex"
)

const defaultLimit = 25

type Handler struct {
	queries *db.Queries
	querier *searchindex.Querier
	logger  *slog.Logger
}

func NewHandler(queries *db.Queries, querier *searchindex.Querier, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, querier: querier, logger: logger}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/spaces/{spaceID}/search", h.search)
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, spaceID, authz.RoleViewer) {
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		httpx.WriteError(w, http.StatusBadRequest, "q query parameter is required")
		return
	}

	results, err := h.querier.Search(r.Context(), spaceID, q, defaultLimit)
	if err != nil {
		h.logger.Error("search failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "search failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, results)
}
