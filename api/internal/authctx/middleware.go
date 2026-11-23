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

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	return token, token != ""
}
