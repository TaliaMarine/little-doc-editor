package authctx

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessionrecord"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessiontoken"
)

// localDevAdminUserID is a fixed, well-known UUID used only for the dev auth
// bypass identity — never a real row in the users table. Stable across
// restarts so foreign-key-bearing rows created during a local dev session
// (pages, attachments, ...) stay attributed to the same "user" instead of a
// new random one every time the process restarts.
var localDevAdminUserID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// Middleware returns the auth middleware for the api service. When devBypass
// is true, every request is treated as a fixed local org-admin identity and
// no real authentication happens at all — this must only ever be enabled on
// a developer's own machine (see config.DevAuthBypass).
//
// Otherwise it validates the `Authorization: Bearer <token>` session token
// (see internal/sessiontoken — the same token collab validates, SPEC.md §4)
// and re-checks the referenced user against the database on every request,
// so a user disabled after a token was issued is rejected immediately rather
// than at next token expiry.
func Middleware(devBypass bool, jwtSecret string, queries *db.Queries, logger *slog.Logger) func(http.Handler) http.Handler {
	if devBypass {
		logger.Warn("╔══════════════════════════════════════════════════════════════╗")
		logger.Warn("║ DEV_AUTH_BYPASS is enabled — all requests run as a fake local  ║")
		logger.Warn("║ org-admin user. NEVER enable this outside local development.  ║")
		logger.Warn("╚══════════════════════════════════════════════════════════════╝")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if devBypass {
				id := Identity{
					UserID:      &localDevAdminUserID,
					DisplayName: "Local Dev Admin",
					IsOrgAdmin:  true,
				}
				next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
				return
			}

			token, ok := bearerToken(r)
			if !ok {
				httpx.WriteError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
				return
			}

			claims, err := sessiontoken.Verify(jwtSecret, token)
			if err != nil {
				httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired session")
				return
			}

			// Deny-list for server-side logout (SPEC.md §8). A DB failure here
			// fails closed: an unreadable revocation list must not silently
			// re-admit revoked sessions.
			revoked, err := queries.IsSessionRevoked(r.Context(), sessionrecord.HashToken(token))
			if err != nil {
				logger.Error("checking session revocation failed", "error", err)
				httpx.WriteError(w, http.StatusInternalServerError, "invalid session")
				return
			}
			if revoked {
				httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired session")
				return
			}

			id, err := resolveIdentity(r, queries, claims)
			if err != nil {
				status := http.StatusUnauthorized
				if !errors.Is(err, errIdentityNotFound) && !errors.Is(err, errUserDisabled) && !errors.Is(err, errShareLinkInactive) {
					status = http.StatusInternalServerError
					logger.Error("resolving identity failed", "error", err)
				}
				httpx.WriteError(w, status, "invalid session")
				return
			}

			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}

var (
	errIdentityNotFound  = errors.New("authctx: identity not found")
	errUserDisabled      = errors.New("authctx: user disabled")
	errShareLinkInactive = errors.New("authctx: share link revoked, expired, or deleted")
)

func resolveIdentity(r *http.Request, queries *db.Queries, claims sessiontoken.Claims) (Identity, error) {
	if claims.Subject != "" {
		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			return Identity{}, errIdentityNotFound
		}
		user, err := queries.GetUserByID(r.Context(), db.ToPgUUID(userID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Identity{}, errIdentityNotFound
			}
			return Identity{}, err
		}
		if user.Disabled {
			return Identity{}, errUserDisabled
		}
		return Identity{UserID: &userID, DisplayName: user.DisplayName, IsOrgAdmin: user.IsOrgAdmin}, nil
	}

	guestID, err := uuid.Parse(claims.GuestIdentityID)
	if err != nil {
		return Identity{}, errIdentityNotFound
	}
	if _, err := queries.GetGuestIdentityByID(r.Context(), db.ToPgUUID(guestID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Identity{}, errIdentityNotFound
		}
		return Identity{}, err
	}

	id := Identity{GuestIdentityID: &guestID, DisplayName: claims.DisplayName}
	if claims.ShareLinkID != "" {
		linkID, err := uuid.Parse(claims.ShareLinkID)
		if err != nil {
			return Identity{}, errIdentityNotFound
		}
		// Re-checked on every request so revoking/expiring a link (SPEC.md
		// §8: "revocable") cuts off guest sessions already issued from it,
		// not just future redemptions.
		link, err := queries.GetShareLinkByID(r.Context(), db.ToPgUUID(linkID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Identity{}, errShareLinkInactive
			}
			return Identity{}, err
		}
		if link.RevokedAt.Valid || (link.ExpiresAt.Valid && link.ExpiresAt.Time.Before(time.Now())) {
			return Identity{}, errShareLinkInactive
		}
		pageID := db.FromPgUUID(link.PageID)
		id.ShareLinkPageID = &pageID
		id.SharePermission = string(link.Permission)
	}
	return id, nil
}

// BearerToken extracts the session token from the Authorization header.
func BearerToken(r *http.Request) (string, bool) { return bearerToken(r) }

// OptionalUserID returns the user ID behind a valid, unrevoked bearer token
// if the request carries one, else nil. For public endpoints (share-link
// redemption) that behave the same for anonymous callers but record who the
// viewer was when they happen to be signed in. Never errors: an invalid or
// absent token just means "anonymous".
func OptionalUserID(r *http.Request, jwtSecret string, queries *db.Queries) *uuid.UUID {
	token, ok := bearerToken(r)
	if !ok {
		return nil
	}
	claims, err := sessiontoken.Verify(jwtSecret, token)
	if err != nil || claims.Subject == "" {
		return nil
	}
	if revoked, err := queries.IsSessionRevoked(r.Context(), sessionrecord.HashToken(token)); err != nil || revoked {
		return nil
	}
	id, err := resolveIdentity(r, queries, claims)
	if err != nil || id.UserID == nil {
		return nil
	}
	return id.UserID
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	return token, token != ""
}
