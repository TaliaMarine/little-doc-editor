// Package authapi implements internal (password) login and the "who am I"
// endpoint. OIDC login lives in internal/oidcapi; share-link redemption
// (which also issues guest sessions) lives in internal/shareapi — both
// issue the same session token this package's middleware validates.
package authapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/passwordhash"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessionrecord"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessiontoken"
)

type Handler struct {
	queries *db.Queries
	cfg     config.Config
	logger  *slog.Logger
	// dummyHash is verified against on "no such user" paths so a login for
	// a nonexistent email costs the same argon2id work as a wrong password
	// — otherwise response timing alone enumerates which emails have
	// accounts.
	dummyHash string
}

func NewHandler(queries *db.Queries, cfg config.Config, logger *slog.Logger) *Handler {
	dummyHash, err := passwordhash.Hash("timing-equalization-dummy-password")
	if err != nil {
		// Only reachable if crypto/rand fails, which already makes every
		// other login path unusable too.
		logger.Error("computing dummy password hash failed", "error", err)
	}
	return &Handler{queries: queries, cfg: cfg, logger: logger, dummyHash: dummyHash}
}

func (h *Handler) burnPasswordCheck(password string) {
	if h.dummyHash != "" {
		_, _ = passwordhash.Verify(password, h.dummyHash)
	}
}

// PublicRoutes are reachable without an existing session — they're how one
// is obtained in the first place.
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Post("/auth/login", h.login)
}

// PrivateRoutes require an existing session, same as every other resource
// route.
func (h *Handler) PrivateRoutes(r chi.Router) {
	r.Get("/auth/me", h.me)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionResponse struct {
	Token       string `json:"token"`
	DisplayName string `json:"displayName"`
	IsOrgAdmin  bool   `json:"isOrgAdmin"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Email == "" || req.Password == "" {
		httpx.WriteError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	settings, err := h.queries.GetInstanceSettings(r.Context())
	if err != nil {
		h.logger.Error("login: fetching instance settings failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "login failed")
		return
	}
	if settings.InternalLoginDisabled && !h.cfg.InternalLoginBreakGlass {
		httpx.WriteError(w, http.StatusForbidden, "internal login is disabled for this instance; use single sign-on")
		return
	}

	user, err := h.queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.burnPasswordCheck(req.Password)
			httpx.WriteError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		h.logger.Error("login: looking up user failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "login failed")
		return
	}
	if user.AuthProvider != db.AuthProviderInternal || user.PasswordHash == nil {
		h.burnPasswordCheck(req.Password)
		httpx.WriteError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	ok, err := passwordhash.Verify(req.Password, *user.PasswordHash)
	if err != nil || !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	// Checked only after the password: reporting "disabled" for a wrong
	// password would confirm the account exists to anyone who asks.
	if user.Disabled {
		httpx.WriteError(w, http.StatusForbidden, "this account has been disabled")
		return
	}

	userID := db.FromPgUUID(user.ID)
	token, err := sessiontoken.IssueForUser(h.cfg.SessionJWTSecret, userID, user.DisplayName, h.cfg.SessionTTL)
	if err != nil {
		h.logger.Error("login: issuing token failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "login failed")
		return
	}

	// Best-effort session-tracking row for auditing; not consulted for
	// revocation yet (see internal/sessiontoken doc comment) — a failure
	// here shouldn't block a successful login.
	if _, err := h.queries.CreateUserSession(r.Context(), db.CreateUserSessionParams{
		UserID:    user.ID,
		TokenHash: sessionrecord.HashToken(token),
		ExpiresAt: sessionrecord.ExpiresAt(h.cfg.SessionTTL),
	}); err != nil {
		h.logger.Warn("login: recording session row failed", "error", err)
	}

	httpx.WriteJSON(w, http.StatusOK, sessionResponse{
		Token:       token,
		DisplayName: user.DisplayName,
		IsOrgAdmin:  user.IsOrgAdmin,
	})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	identity := authctx.FromContext(r.Context())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"displayName": identity.DisplayName,
		"isOrgAdmin":  identity.IsOrgAdmin,
		"isGuest":     identity.IsGuest(),
	})
}
