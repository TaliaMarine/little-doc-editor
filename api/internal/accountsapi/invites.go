// Package accountsapi implements account management without SMTP (Decisions
// 11, 15): admin-issued invite links, the public invite redemption that
// creates an internal (argon2id) account, and the org-admin user directory.
package accountsapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/passwordhash"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessionrecord"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessiontoken"
)

const (
	// DefaultInviteTTL applies when the admin gives no expiry: an invite is a
	// bearer credential, so it never lives forever by accident.
	DefaultInviteTTL = 7 * 24 * time.Hour
	MaxInviteTTL     = 90 * 24 * time.Hour

	minPasswordLength   = 10
	maxPasswordLength   = 128 // bounds argon2id work per request
	maxDisplayNameRunes = 80
	maxEmailLength      = 254
)

type Handler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	cfg     config.Config
	logger  *slog.Logger
}

func NewHandler(pool *pgxpool.Pool, queries *db.Queries, cfg config.Config, logger *slog.Logger) *Handler {
	return &Handler{pool: pool, queries: queries, cfg: cfg, logger: logger}
}

// PublicRoutes need no session: they are how an invited person gets one.
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Get("/invites/{token}", h.resolve)
	r.Post("/invites/{token}/redeem", h.redeem)
}

// PrivateRoutes are org-admin only.
func (h *Handler) PrivateRoutes(r chi.Router) {
	r.Post("/invites", h.create)
	r.Get("/invites", h.list)
	r.Delete("/invites/{inviteID}", h.revoke)
	r.Get("/users", h.listUsers)
	r.Patch("/users/{userID}", h.updateUser)
}

func (h *Handler) requireOrgAdmin(w http.ResponseWriter, r *http.Request) (authctx.Identity, bool) {
	id := authctx.FromContext(r.Context())
	if !id.IsOrgAdmin {
		httpx.WriteError(w, http.StatusForbidden, "org admin required")
		return id, false
	}
	return id, true
}

// HashToken is the stored form of an invite token (hex SHA-256). The tokens
// are 256-bit random values, so a fast unsalted hash is sufficient.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func inviteStatus(redeemedAt, revokedAt, expiresAt pgtype.Timestamptz) string {
	switch {
	case redeemedAt.Valid:
		return "redeemed"
	case revokedAt.Valid:
		return "revoked"
	case expiresAt.Valid && expiresAt.Time.Before(time.Now()):
		return "expired"
	default:
		return "pending"
	}
}

type inviteResponse struct {
	ID                   pgtype.UUID        `json:"id"`
	Email                *string            `json:"email"`
	GrantsOrgAdmin       bool               `json:"grants_org_admin"`
	Status               string             `json:"status"`
	ExpiresAt            pgtype.Timestamptz `json:"expires_at"`
	RedeemedAt           pgtype.Timestamptz `json:"redeemed_at"`
	RedeemedByUserID     pgtype.UUID        `json:"redeemed_by_user_id"`
	RevokedAt            pgtype.Timestamptz `json:"revoked_at"`
	CreatedByUserID      pgtype.UUID        `json:"created_by_user_id"`
	CreatedByDisplayName *string            `json:"created_by_display_name"`
	CreatedAt            pgtype.Timestamptz `json:"created_at"`
}

// inviteCreatedResponse additionally carries the plaintext token — returned
// exactly once, at creation; only its hash is stored, so it can't be shown
// again.
type inviteCreatedResponse struct {
	inviteResponse
	Token string `json:"token"`
}

type createInviteRequest struct {
	Email          string  `json:"email"`
	GrantsOrgAdmin bool    `json:"grantsOrgAdmin"`
	ExpiresAt      *string `json:"expiresAt"` // RFC3339; default now+7d, max now+90d
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}
	if identity.UserID == nil {
		httpx.WriteError(w, http.StatusForbidden, "org admin required")
		return
	}
	var req createInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if blocked, err := h.internalAccountsBlocked(r); err != nil {
		h.logger.Error("create invite: reading instance settings failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create invite")
		return
	} else if blocked {
		httpx.WriteError(w, http.StatusConflict, "internal login is disabled for this instance; invites would create accounts that cannot sign in")
		return
	}

	var email *string
	if req.Email != "" {
		norm, err := normalizeEmail(req.Email)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := h.queries.GetUserByEmailAnyProvider(r.Context(), norm); err == nil {
			httpx.WriteError(w, http.StatusConflict, "an account with this email already exists")
			return
		} else if !errors.Is(err, pgx.ErrNoRows) {
			h.logger.Error("create invite: user lookup failed", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "failed to create invite")
			return
		}
		email = &norm
	}

	expires := time.Now().Add(DefaultInviteTTL)
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "expiresAt must be RFC3339")
			return
		}
		if !t.After(time.Now()) || t.After(time.Now().Add(MaxInviteTTL)) {
			httpx.WriteError(w, http.StatusBadRequest, "expiresAt must be in the future and at most 90 days away")
			return
		}
		expires = t
	}

	token, err := generateToken()
	if err != nil {
		h.logger.Error("generating invite token failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create invite")
		return
	}
	inv, err := h.queries.CreateInvite(r.Context(), db.CreateInviteParams{
		TokenHash:       HashToken(token),
		Email:           email,
		GrantsOrgAdmin:  req.GrantsOrgAdmin,
		CreatedByUserID: db.ToPgUUID(*identity.UserID),
		ExpiresAt:       pgtype.Timestamptz{Time: expires, Valid: true},
	})
	if err != nil {
		h.logger.Error("create invite failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create invite")
		return
	}
	name := identity.DisplayName
	httpx.WriteJSON(w, http.StatusCreated, inviteCreatedResponse{
		inviteResponse: inviteResponse{
			ID: inv.ID, Email: inv.Email, GrantsOrgAdmin: inv.GrantsOrgAdmin,
			Status:    inviteStatus(inv.RedeemedAt, inv.RevokedAt, inv.ExpiresAt),
			ExpiresAt: inv.ExpiresAt, RedeemedAt: inv.RedeemedAt, RedeemedByUserID: inv.RedeemedByUserID,
			RevokedAt: inv.RevokedAt, CreatedByUserID: inv.CreatedByUserID,
			CreatedByDisplayName: &name, CreatedAt: inv.CreatedAt,
		},
		Token: token,
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireOrgAdmin(w, r); !ok {
		return
	}
	rows, err := h.queries.ListInvites(r.Context())
	if err != nil {
		h.logger.Error("list invites failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list invites")
		return
	}
	out := make([]inviteResponse, len(rows))
	for i, inv := range rows {
		out[i] = inviteResponse{
			ID: inv.ID, Email: inv.Email, GrantsOrgAdmin: inv.GrantsOrgAdmin,
			Status:    inviteStatus(inv.RedeemedAt, inv.RevokedAt, inv.ExpiresAt),
			ExpiresAt: inv.ExpiresAt, RedeemedAt: inv.RedeemedAt, RedeemedByUserID: inv.RedeemedByUserID,
			RevokedAt: inv.RevokedAt, CreatedByUserID: inv.CreatedByUserID,
			CreatedByDisplayName: inv.CreatedByDisplayName, CreatedAt: inv.CreatedAt,
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireOrgAdmin(w, r); !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "inviteID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid invite id")
		return
	}
	if _, err := h.queries.RevokeInvite(r.Context(), db.ToPgUUID(id)); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			h.logger.Error("revoke invite failed", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "failed to revoke invite")
			return
		}
		// Either unknown or no longer pending.
		if _, gerr := h.queries.GetInviteByID(r.Context(), db.ToPgUUID(id)); gerr != nil {
			httpx.WriteLookupError(w, h.logger, gerr, "invite not found")
			return
		}
		httpx.WriteError(w, http.StatusConflict, "invite is no longer pending")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// internalAccountsBlocked: when internal login is disabled instance-wide, a
// freshly invited internal account could never sign in.
func (h *Handler) internalAccountsBlocked(r *http.Request) (bool, error) {
	settings, err := h.queries.GetInstanceSettings(r.Context())
	if err != nil {
		return false, err
	}
	return settings.InternalLoginDisabled, nil
}

func normalizeEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxEmailLength {
		return "", errors.New("a valid email address is required")
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw {
		return "", errors.New("a valid email address is required")
	}
	norm := strings.ToLower(raw)
	// ".invalid" is reserved (RFC 2606) and is what OIDC placeholder emails
	// use; accepting it would let an invite pre-block an SSO user's address.
	if strings.HasSuffix(norm, ".invalid") {
		return "", errors.New("a valid email address is required")
	}
	return norm, nil
}

type resolveResponse struct {
	Email     *string            `json:"email"`
	ExpiresAt pgtype.Timestamptz `json:"expiresAt"`
}

// loadUsable looks the invite up by token and maps unusable states to HTTP
// errors: unknown -> 404, redeemed/revoked/expired -> 410.
func (h *Handler) loadUsable(w http.ResponseWriter, r *http.Request) (db.Invite, bool) {
	inv, err := h.queries.GetInviteByTokenHash(r.Context(), HashToken(chi.URLParam(r, "token")))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "invite not found")
		return db.Invite{}, false
	}
	switch inviteStatus(inv.RedeemedAt, inv.RevokedAt, inv.ExpiresAt) {
	case "redeemed":
		httpx.WriteError(w, http.StatusGone, "this invite has already been used")
		return db.Invite{}, false
	case "revoked":
		httpx.WriteError(w, http.StatusGone, "this invite has been revoked")
		return db.Invite{}, false
	case "expired":
		httpx.WriteError(w, http.StatusGone, "this invite has expired")
		return db.Invite{}, false
	}
	return inv, true
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	inv, ok := h.loadUsable(w, r)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resolveResponse{Email: inv.Email, ExpiresAt: inv.ExpiresAt})
}

type redeemRequest struct {
	Email       string `json:"email"` // required only when the invite carries none
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

func (h *Handler) redeem(w http.ResponseWriter, r *http.Request) {
	inv, ok := h.loadUsable(w, r)
	if !ok {
		return
	}
	var req redeemRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" || utf8.RuneCountInString(displayName) > maxDisplayNameRunes {
		httpx.WriteError(w, http.StatusBadRequest, "displayName is required (max 80 characters)")
		return
	}
	if n := utf8.RuneCountInString(req.Password); n < minPasswordLength || len(req.Password) > maxPasswordLength {
		httpx.WriteError(w, http.StatusBadRequest, "password must be 10 to 128 characters")
		return
	}
	var email string
	if inv.Email != nil {
		email = *inv.Email // an invite bound to an address always creates that account
	} else {
		var err error
		if email, err = normalizeEmail(req.Email); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if blocked, err := h.internalAccountsBlocked(r); err != nil {
		h.logger.Error("redeem invite: reading instance settings failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem invite")
		return
	} else if blocked {
		httpx.WriteError(w, http.StatusConflict, "internal login is disabled for this instance")
		return
	}

	hash, err := passwordhash.Hash(req.Password)
	if err != nil {
		h.logger.Error("redeem invite: hashing password failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem invite")
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.logger.Error("redeem invite: begin tx failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem invite")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := h.queries.WithTx(tx)

	// Atomic claim: of two concurrent redemptions exactly one gets a row.
	claimed, err := q.ClaimInvite(r.Context(), HashToken(chi.URLParam(r, "token")))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteError(w, http.StatusGone, "this invite is no longer usable")
			return
		}
		h.logger.Error("redeem invite: claim failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem invite")
		return
	}

	if _, err := q.GetUserByEmailAnyProvider(r.Context(), email); err == nil {
		httpx.WriteError(w, http.StatusConflict, "an account with this email already exists")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		h.logger.Error("redeem invite: user lookup failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem invite")
		return
	}

	var user db.User
	if claimed.GrantsOrgAdmin {
		user, err = q.CreateInternalOrgAdmin(r.Context(), db.CreateInternalOrgAdminParams{Email: email, DisplayName: displayName, PasswordHash: &hash})
	} else {
		user, err = q.CreateInternalUser(r.Context(), db.CreateInternalUserParams{Email: email, DisplayName: displayName, PasswordHash: &hash})
	}
	if err != nil {
		if db.IsUniqueViolation(err) {
			httpx.WriteError(w, http.StatusConflict, "an account with this email already exists")
			return
		}
		h.logger.Error("redeem invite: creating user failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem invite")
		return
	}
	if err := q.SetInviteRedeemedBy(r.Context(), db.SetInviteRedeemedByParams{ID: claimed.ID, RedeemedByUserID: user.ID}); err != nil {
		h.logger.Error("redeem invite: recording redemption failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem invite")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.logger.Error("redeem invite: commit failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem invite")
		return
	}

	// Sign the new user in right away.
	token, err := sessiontoken.IssueForUser(h.cfg.SessionJWTSecret, db.FromPgUUID(user.ID), user.DisplayName, h.cfg.SessionTTL)
	if err != nil {
		h.logger.Error("redeem invite: issuing token failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "account created, but sign-in failed; log in with your new credentials")
		return
	}
	if _, err := h.queries.CreateUserSession(r.Context(), db.CreateUserSessionParams{
		UserID: user.ID, TokenHash: sessionrecord.HashToken(token), ExpiresAt: sessionrecord.ExpiresAt(h.cfg.SessionTTL),
	}); err != nil {
		h.logger.Warn("redeem invite: recording session row failed", "error", err)
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"token": token, "displayName": user.DisplayName, "isOrgAdmin": user.IsOrgAdmin,
	})
}
