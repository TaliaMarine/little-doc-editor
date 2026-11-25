package accountsapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
)

type userResponse struct {
	ID           pgtype.UUID        `json:"id"`
	Email        string             `json:"email"`
	DisplayName  string             `json:"display_name"`
	AuthProvider db.AuthProvider    `json:"auth_provider"`
	IsOrgAdmin   bool               `json:"is_org_admin"`
	Disabled     bool               `json:"disabled"`
	CreatedAt    pgtype.Timestamptz `json:"created_at"`
	LastLoginAt  pgtype.Timestamptz `json:"last_login_at"`
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireOrgAdmin(w, r); !ok {
		return
	}
	limit, offset := pagination(r, 100, 500)
	params := db.ListUsersParams{Limit: limit, Offset: offset}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		esc := httpx.EscapeLike(q)
		params.Q = &esc
	}
	rows, err := h.queries.ListUsers(r.Context(), params)
	if err != nil {
		h.logger.Error("list users failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	out := make([]userResponse, len(rows))
	for i, u := range rows {
		out[i] = userResponse{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, AuthProvider: u.AuthProvider,
			IsOrgAdmin: u.IsOrgAdmin, Disabled: u.Disabled, CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// pagination parses limit/offset query params with a default and a ceiling.
func pagination(r *http.Request, def, max int32) (limit, offset int32) {
	limit = def
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = int32(n)
	}
	if limit > max {
		limit = max
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n > 0 {
		offset = int32(n)
	}
	return limit, offset
}

type updateUserRequest struct {
	Disabled *bool `json:"disabled"`
}

// updateUser enables/disables an account. Disabling takes effect on the very
// next request (authctx.Middleware re-reads users.disabled) and collab's
// per-message recheck. An admin can't disable themselves, and the last
// enabled org admin can never be disabled (the instance would be unmanageable).
func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Disabled == nil {
		httpx.WriteError(w, http.StatusBadRequest, "disabled is required")
		return
	}
	if *req.Disabled && identity.UserID != nil && *identity.UserID == targetID {
		httpx.WriteError(w, http.StatusConflict, "you cannot disable your own account")
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.logger.Error("update user: begin tx failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := h.queries.WithTx(tx)

	if err := q.LockOrgAdminChanges(r.Context()); err != nil {
		h.logger.Error("update user: lock failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	target, err := q.GetUserByID(r.Context(), db.ToPgUUID(targetID))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "user not found")
		return
	}
	if *req.Disabled && target.IsOrgAdmin && !target.Disabled {
		n, err := q.CountOrgAdmins(r.Context())
		if err != nil {
			h.logger.Error("update user: counting admins failed", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "failed to update user")
			return
		}
		if n <= 1 {
			httpx.WriteError(w, http.StatusConflict, "cannot disable the last enabled org admin")
			return
		}
	}
	if err := q.SetUserDisabled(r.Context(), db.SetUserDisabledParams{ID: target.ID, Disabled: *req.Disabled}); err != nil {
		h.logger.Error("update user: set disabled failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	updated, err := q.GetUserByID(r.Context(), target.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		h.logger.Error("update user: reload failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.logger.Error("update user: commit failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, userResponse{ID: updated.ID, Email: updated.Email, DisplayName: updated.DisplayName,
		AuthProvider: updated.AuthProvider, IsOrgAdmin: updated.IsOrgAdmin, Disabled: updated.Disabled,
		CreatedAt: updated.CreatedAt, LastLoginAt: updated.LastLoginAt})
}
