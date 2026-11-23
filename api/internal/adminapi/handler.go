// Package adminapi implements instance-wide admin settings (SPEC.md
// Decision 14) — currently just the internal-login disable toggle.
package adminapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
)

type Handler struct {
	queries *db.Queries
	logger  *slog.Logger
}

func NewHandler(queries *db.Queries, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, logger: logger}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/admin/settings", h.get)
	r.Patch("/admin/settings", h.update)
}

func (h *Handler) requireOrgAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !authctx.FromContext(r.Context()).IsOrgAdmin {
		httpx.WriteError(w, http.StatusForbidden, "org admin required")
		return false
	}
	return true
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgAdmin(w, r) {
		return
	}
	settings, err := h.queries.GetInstanceSettings(r.Context())
	if err != nil {
		h.logger.Error("get instance settings failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, settings)
}

type updateSettingsRequest struct {
	InternalLoginDisabled *bool `json:"internalLoginDisabled"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgAdmin(w, r) {
		return
	}
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.InternalLoginDisabled == nil {
		httpx.WriteError(w, http.StatusBadRequest, "internalLoginDisabled is required")
		return
	}

	settings, err := h.queries.SetInternalLoginDisabled(r.Context(), *req.InternalLoginDisabled)
	if err != nil {
		h.logger.Error("update instance settings failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, settings)
}
