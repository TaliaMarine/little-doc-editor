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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/passwordhash"
	"github.com/TaliaMarine/little-doc-editor/api/internal/ratelimit"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessionrecord"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessiontoken"
)

type Handler struct {
	queries *db.Queries
	cfg     config.Config
	logger  *slog.Logger
	limiter *ratelimit.Limiter
	// dummyHash is verified against on "no such user" paths so a login for
	// a nonexistent email costs the same argon2id work as a wrong password
	// — otherwise response timing alone enumerates which emails have
	// accounts.
	dummyHash string
}

func NewHandler(queries *db.Queries, cfg config.Config, logger *slog.Logger, limiter *ratelimit.Limiter) *Handler {
	dummyHash, err := passwordhash.Hash("timing-equalization-dummy-password")
	if err != nil {
		// Only reachable if crypto/rand fails, which already makes every
		// other login path unusable too.
		logger.Error("computing dummy password hash failed", "error", err)
	}
	return &Handler{queries: queries, cfg: cfg, logger: logger, limiter: limiter, dummyHash: dummyHash}
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
	r.Post("/auth/logout", h.logout)
}

const (
	maxLoginPasswordBytes = 1024
	maxLoginFailures      = 10
)

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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Email == "" || req.Password == "" {
		httpx.WriteError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if len(req.Password) > maxLoginPasswordBytes {
		httpx.WriteError(w, http.StatusBadRequest, "password is too long")
		return
	}

	// Failed-attempt limiter per (account, client network), on top of the
	// per-IP request limiter applied at the router. Only failures count, and
	// the key includes the client, so a stranger hammering an email cannot
	// lock the real owner (or the break-glass admin) out from elsewhere.
	failKey := strings.ToLower(strings.TrimSpace(req.Email)) + "|" + h.limiter.ClientKey(r)
	if n, retry := h.limiter.Count(r.Context(), "login-fail", failKey); n >= maxLoginFailures {
		ratelimit.Reject(w, retry)
		return
	}
	fail := func() { h.limiter.Allow(r.Context(), "login-fail", failKey, maxLoginFailures, 15*time.Minute) }

	settings, err := h.queries.GetInstanceSettings(r.Context())
	if err != nil {
		h.logger.Error("login: fetching instance settings failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "login failed")
		return
	}
	if settings.InternalLoginDisabled && !h.breakGlassAllows(req.Email) {
		httpx.WriteError(w, http.StatusForbidden, "internal login is disabled for this instance; use single sign-on")
		return
	}

	user, err := h.queries.GetUserByEmailAnyProvider(r.Context(), strings.TrimSpace(req.Email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.burnPasswordCheck(req.Password)
			fail()
			httpx.WriteError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		h.logger.Error("login: looking up user failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "login failed")
		return
	}
	if user.AuthProvider != db.AuthProviderInternal || user.PasswordHash == nil {
		h.burnPasswordCheck(req.Password)
		fail()
		httpx.WriteError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	ok, err := passwordhash.Verify(req.Password, *user.PasswordHash)
	if err != nil || !ok {
		fail()
		httpx.WriteError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	// Break-glass is for the bootstrap admin, who is an org admin: an ordinary
	// account that merely registered the same address (e.g. through an unbound
	// invite) doesn't qualify.
	if settings.InternalLoginDisabled && !user.IsOrgAdmin {
		httpx.WriteError(w, http.StatusForbidden, "internal login is disabled for this instance; use single sign-on")
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

	// Best-effort session-tracking row for auditing. Revocation is a
	// deny-list (a row with revoked_at set), so a failure here doesn't block
	// login and doesn't weaken logout either — logout upserts the row.
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

// breakGlassAllows reports whether email may log in with a password while
// internal login is disabled instance-wide: only when the operator set
// INTERNAL_LOGIN_BREAK_GLASS on the pod, and only for the bootstrap admin
// (Decision 14) — not every internal account. The answer depends only on the
// submitted email string, so it reveals nothing about which accounts exist.
func (h *Handler) breakGlassAllows(email string) bool {
	return h.cfg.InternalLoginBreakGlass &&
		h.cfg.BootstrapAdminEmail != "" &&
		strings.EqualFold(strings.TrimSpace(email), h.cfg.BootstrapAdminEmail)
}

// logout revokes the presented session token server-side (deny-list in the
// sessions table, checked by authctx.Middleware; collab checks the same
// row). Idempotent. Under DEV_AUTH_BYPASS there may be no token: no-op.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	token, ok := authctx.BearerToken(r)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	claims, err := sessiontoken.Verify(h.cfg.SessionJWTSecret, token)
	if err != nil {
		w.WriteHeader(http.StatusNoContent) // nothing valid to revoke
		return
	}
	params := db.RevokeSessionParams{
		TokenHash: sessionrecord.HashToken(token),
		ExpiresAt: pgtype.Timestamptz{Time: claims.ExpiresAt.Time, Valid: true},
	}
	if claims.Subject != "" {
		uid, err := uuid.Parse(claims.Subject)
		if err != nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		params.UserID = db.ToPgUUID(uid)
	} else {
		gid, err := uuid.Parse(claims.GuestIdentityID)
		if err != nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		params.GuestIdentityID = db.ToPgUUID(gid)
	}
	if err := h.queries.RevokeSession(r.Context(), params); err != nil {
		if db.IsForeignKeyViolation(err) { // the account was deleted meanwhile
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.logger.Error("logout: revoking session failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "logout failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	identity := authctx.FromContext(r.Context())
	out := map[string]any{
		"displayName": identity.DisplayName,
		"isOrgAdmin":  identity.IsOrgAdmin,
		"isGuest":     identity.IsGuest(),
	}
	if identity.UserID != nil {
		if user, err := h.queries.GetUserByID(r.Context(), db.ToPgUUID(*identity.UserID)); err == nil {
			out["email"] = user.Email
			out["authProvider"] = user.AuthProvider
			out["username"] = user.Username
			out["avatarUrl"] = user.AvatarUrl
			out["groups"] = user.IdpGroups
			if user.LastLoginAt.Valid {
				out["lastLoginAt"] = user.LastLoginAt.Time
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
