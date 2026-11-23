// Package shareapi implements share links (SPEC.md §6.6, Decisions 16, 21):
// opaque-token page sharing that grants anonymous (guest) access to a page
// and its descendants at a view or edit permission, optionally
// password/expiry-gated, with an access log visible to the page's editors
// and the space's admins/owners.
package shareapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authz"
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
}

func NewHandler(queries *db.Queries, cfg config.Config, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, cfg: cfg, logger: logger}
}

// PublicRoutes need no existing session — redeeming a link is how an
// anonymous visitor gets one (SPEC.md Decision 6).
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Get("/share/{token}", h.resolve)
	r.Post("/share/{token}/redeem", h.redeem)
}

// PrivateRoutes manage links and require an existing session with
// sufficient space role. Share-link guests never qualify, even with an edit
// link — managing links is a space-member action.
func (h *Handler) PrivateRoutes(r chi.Router) {
	r.Post("/pages/{pageID}/share-links", h.create)
	r.Get("/pages/{pageID}/share-links", h.list)
	r.Delete("/share-links/{id}", h.revoke)
	r.Get("/share-links/{id}/access-log", h.accessLog)
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// maxDisplayNameLength bounds the guest display name (Decision 6) — it's
// shown to other collaborators and stored in access logs, so it shouldn't be
// an unbounded attacker-controlled string.
const maxDisplayNameLength = 80

// shareLinkResponse is db.ShareLink minus password_hash — the argon2id hash
// of a link's password is never something an API consumer needs, and
// exposing it would allow offline guessing of the password.
type shareLinkResponse struct {
	ID               pgtype.UUID            `json:"id"`
	PageID           pgtype.UUID            `json:"page_id"`
	Token            string                 `json:"token"`
	Permission       db.ShareLinkPermission `json:"permission"`
	RequiresPassword bool                   `json:"requires_password"`
	ExpiresAt        pgtype.Timestamptz     `json:"expires_at"`
	RevokedAt        pgtype.Timestamptz     `json:"revoked_at"`
	CreatedByUserID  pgtype.UUID            `json:"created_by_user_id"`
	CreatedAt        pgtype.Timestamptz     `json:"created_at"`
}

func toShareLinkResponse(l db.ShareLink) shareLinkResponse {
	return shareLinkResponse{
		ID:               l.ID,
		PageID:           l.PageID,
		Token:            l.Token,
		Permission:       l.Permission,
		RequiresPassword: l.PasswordHash != nil,
		ExpiresAt:        l.ExpiresAt,
		RevokedAt:        l.RevokedAt,
		CreatedByUserID:  l.CreatedByUserID,
		CreatedAt:        l.CreatedAt,
	}
}

type createShareLinkRequest struct {
	Permission string  `json:"permission"` // "view" or "edit"
	Password   string  `json:"password,omitempty"`
	ExpiresAt  *string `json:"expiresAt,omitempty"` // RFC3339
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	pageID, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}
	page, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(pageID))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "page not found")
		return
	}

	identity := authctx.FromContext(r.Context())
	if identity.UserID == nil {
		httpx.WriteError(w, http.StatusForbidden, "only authenticated users can create share links")
		return
	}
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, db.FromPgUUID(page.SpaceID), authz.RoleEditor) {
		return
	}

	var req createShareLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	var permission db.ShareLinkPermission
	switch req.Permission {
	case "view":
		permission = db.ShareLinkPermissionView
	case "edit":
		permission = db.ShareLinkPermissionEdit
	default:
		httpx.WriteError(w, http.StatusBadRequest, `permission must be "view" or "edit"`)
		return
	}

	token, err := generateToken()
	if err != nil {
		h.logger.Error("generating share token failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create share link")
		return
	}

	params := db.CreateShareLinkParams{
		PageID:          db.ToPgUUID(pageID),
		Token:           token,
		Permission:      permission,
		CreatedByUserID: db.ToPgUUID(*identity.UserID),
	}
	if req.Password != "" {
		hash, err := passwordhash.Hash(req.Password)
		if err != nil {
			h.logger.Error("hashing share link password failed", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "failed to create share link")
			return
		}
		params.PasswordHash = &hash
	}
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "expiresAt must be RFC3339")
			return
		}
		params.ExpiresAt = pgtype.Timestamptz{Time: t, Valid: true}
	}

	link, err := h.queries.CreateShareLink(r.Context(), params)
	if err != nil {
		h.logger.Error("create share link failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create share link")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toShareLinkResponse(link))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	pageID, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}
	page, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(pageID))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "page not found")
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, db.FromPgUUID(page.SpaceID), authz.RoleEditor) {
		return
	}

	links, err := h.queries.ListShareLinksForPage(r.Context(), db.ToPgUUID(pageID))
	if err != nil {
		h.logger.Error("list share links failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list share links")
		return
	}
	resp := make([]shareLinkResponse, len(links))
	for i, l := range links {
		resp[i] = toShareLinkResponse(l)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid share link id")
		return
	}
	page, ok := h.loadLinkPage(w, r, id)
	if !ok {
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, db.FromPgUUID(page.SpaceID), authz.RoleEditor) {
		return
	}

	if err := h.queries.RevokeShareLink(r.Context(), db.ToPgUUID(id)); err != nil {
		h.logger.Error("revoke share link failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to revoke share link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// accessLog visibility: space admins/owners and the page's editors — i.e.
// editor tier or above in the space, since admin/owner both meet an editor
// minimum (SPEC.md Decision 16). Deliberately RequireSpaceRole, not
// RequirePageAccess: an edit-link guest is not one of "the page's editors"
// for link-management purposes, and must not see other links' tokens or
// their access logs.
func (h *Handler) accessLog(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid share link id")
		return
	}
	page, ok := h.loadLinkPage(w, r, id)
	if !ok {
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, db.FromPgUUID(page.SpaceID), authz.RoleEditor) {
		return
	}

	logs, err := h.queries.ListShareLinkAccessLogs(r.Context(), db.ToPgUUID(id))
	if err != nil {
		h.logger.Error("list share link access logs failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to load access log")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, logs)
}

// loadLinkPage returns the page a share link (by ID) belongs to, writing a
// 404 if either doesn't exist.
func (h *Handler) loadLinkPage(w http.ResponseWriter, r *http.Request, id uuid.UUID) (db.Page, bool) {
	link, err := h.queries.GetShareLinkByID(r.Context(), db.ToPgUUID(id))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "share link not found")
		return db.Page{}, false
	}
	page, err := h.queries.GetPageByID(r.Context(), link.PageID)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "page not found")
		return db.Page{}, false
	}
	return page, true
}

type resolveResponse struct {
	RequiresPassword bool   `json:"requiresPassword"`
	Permission       string `json:"permission"`
	PageTitle        string `json:"pageTitle,omitempty"`
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	link, err := h.queries.GetShareLinkByToken(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "share link not found")
		return
	}
	if status, msg := linkStatus(link); status != 0 {
		httpx.WriteError(w, status, msg)
		return
	}

	resp := resolveResponse{
		RequiresPassword: link.PasswordHash != nil,
		Permission:       string(link.Permission),
	}
	if link.PasswordHash == nil {
		if page, err := h.queries.GetPageByID(r.Context(), link.PageID); err == nil {
			resp.PageTitle = page.Title
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

type redeemRequest struct {
	Password    string `json:"password,omitempty"`
	DisplayName string `json:"displayName"`
}

func (h *Handler) redeem(w http.ResponseWriter, r *http.Request) {
	link, err := h.queries.GetShareLinkByToken(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "share link not found")
		return
	}
	if status, msg := linkStatus(link); status != 0 {
		httpx.WriteError(w, status, msg)
		return
	}

	var req redeemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if link.PasswordHash != nil {
		ok, err := passwordhash.Verify(req.Password, *link.PasswordHash)
		if err != nil || !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "incorrect password")
			return
		}
	}

	// Mandatory display-name prompt before anonymous access (Decision 6) —
	// enforced here, server-side, not just by the frontend showing a
	// dialog, so hitting this endpoint directly can't skip it.
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		httpx.WriteError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	if utf8.RuneCountInString(displayName) > maxDisplayNameLength {
		httpx.WriteError(w, http.StatusBadRequest, "displayName is too long")
		return
	}

	guest, err := h.queries.CreateGuestIdentity(r.Context(), displayName)
	if err != nil {
		h.logger.Error("creating guest identity failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem share link")
		return
	}
	guestID := db.FromPgUUID(guest.ID)
	pageID := db.FromPgUUID(link.PageID)

	token, err := sessiontoken.IssueForShareGuest(h.cfg.SessionJWTSecret, guestID, db.FromPgUUID(link.ID), displayName, h.cfg.SessionTTL)
	if err != nil {
		h.logger.Error("issuing guest token failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to redeem share link")
		return
	}

	if _, err := h.queries.CreateGuestSession(r.Context(), db.CreateGuestSessionParams{
		GuestIdentityID: guest.ID,
		TokenHash:       sessionrecord.HashToken(token),
		ExpiresAt:       sessionrecord.ExpiresAt(h.cfg.SessionTTL),
	}); err != nil {
		h.logger.Warn("recording guest session row failed", "error", err)
	}

	h.logAccess(r, link.ID, nil, &guest.ID)

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"permission": string(link.Permission),
		"pageId":     pageID,
	})
}

// linkStatus returns a non-zero HTTP status (and message) if link can't be
// used right now — revoked or expired — else (0, "").
func linkStatus(link db.ShareLink) (int, string) {
	if link.RevokedAt.Valid {
		return http.StatusGone, "this share link has been revoked"
	}
	if link.ExpiresAt.Valid && link.ExpiresAt.Time.Before(time.Now()) {
		return http.StatusGone, "this share link has expired"
	}
	return 0, ""
}

func (h *Handler) logAccess(r *http.Request, linkID pgtype.UUID, userID, guestID *pgtype.UUID) {
	params := db.CreateShareLinkAccessLogParams{
		ShareLinkID: linkID,
		IpHash:      h.hashIP(r),
		UserAgent:   httpx.PtrOrNil(r.UserAgent()),
	}
	if userID != nil {
		params.ViewerUserID = *userID
	}
	if guestID != nil {
		params.ViewerGuestID = *guestID
	}
	if _, err := h.queries.CreateShareLinkAccessLog(r.Context(), params); err != nil {
		h.logger.Warn("recording share link access log failed", "error", err)
	}
}

// hashIP never stores a raw IP address (SPEC.md §8) — only a keyed hash,
// so the log can't be used to directly re-identify a viewer's address, while
// still letting an admin correlate repeated access from the same client. A
// plain unkeyed SHA-256 would not achieve that: the whole IPv4 space is small
// enough to brute-force back to the address in minutes, so the hash is an
// HMAC keyed with a secret derived from the session signing secret.
func (h *Handler) hashIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	mac := hmac.New(sha256.New, []byte("share-access-log-ip:"+h.cfg.SessionJWTSecret))
	mac.Write([]byte(host))
	return hex.EncodeToString(mac.Sum(nil))
}
