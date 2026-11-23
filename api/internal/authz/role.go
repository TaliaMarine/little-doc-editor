// Package authz implements SPEC.md's space-level role model (Decision 7,
// 24): owner/admin manage members/roles/settings; editor creates/edits/
// deletes content; viewer is read-only. There are no per-page ACLs.
package authz

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
)

type Role int

const (
	RoleViewer Role = iota
	RoleEditor
	RoleAdmin
	RoleOwner
)

func parseRole(s db.SpaceRole) (Role, bool) {
	switch s {
	case db.SpaceRoleViewer:
		return RoleViewer, true
	case db.SpaceRoleEditor:
		return RoleEditor, true
	case db.SpaceRoleAdmin:
		return RoleAdmin, true
	case db.SpaceRoleOwner:
		return RoleOwner, true
	default:
		return 0, false
	}
}

// MeetsMinimum reports whether r is at least as privileged as min. Admin and
// owner are treated as equal tier for permission checks (both manage
// members/settings, per Decision 24) — this ordering only matters for the
// editor/viewer boundary and the "can manage members" boundary.
func (r Role) MeetsMinimum(min Role) bool {
	return r >= min
}

// HasSpaceRole reports whether identity has at least min role in space.
// An org admin always satisfies any role check (§6.5) — that's the only
// reading of "org-level admin manages spaces" that doesn't require seeding
// a space_members row for every space on every org admin. Guests never
// have a space role.
func HasSpaceRole(ctx context.Context, queries *db.Queries, identity authctx.Identity, spaceID uuid.UUID, min Role) (bool, error) {
	if identity.IsOrgAdmin {
		return true, nil
	}
	if identity.UserID == nil {
		return false, nil
	}

	dbRole, err := queries.GetSpaceMemberRole(ctx, db.GetSpaceMemberRoleParams{
		SpaceID: db.ToPgUUID(spaceID),
		UserID:  db.ToPgUUID(*identity.UserID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	role, ok := parseRole(dbRole)
	if !ok {
		return false, nil
	}
	return role.MeetsMinimum(min), nil
}

// EffectiveSpaceRole is the space role identity is treated as having, for
// reporting to the UI (my_role in API responses) so it can hide what the
// server would refuse. Org admins satisfy every space-role check without a
// membership row, so they're reported as owner. nil means no role: a
// non-member, or any guest (share-link guests are scoped to pages, not
// spaces — see PageCapabilities).
func EffectiveSpaceRole(ctx context.Context, queries *db.Queries, identity authctx.Identity, spaceID uuid.UUID) (*db.SpaceRole, error) {
	if identity.IsOrgAdmin {
		owner := db.SpaceRoleOwner
		return &owner, nil
	}
	if identity.UserID == nil {
		return nil, nil
	}
	role, err := queries.GetSpaceMemberRole(ctx, db.GetSpaceMemberRoleParams{
		SpaceID: db.ToPgUUID(spaceID),
		UserID:  db.ToPgUUID(*identity.UserID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &role, nil
}

// RequireSpaceRole is HasSpaceRole plus writing the appropriate HTTP error
// response on failure, to cut boilerplate in handlers. Returns true if the
// caller should proceed.
func RequireSpaceRole(ctx context.Context, w http.ResponseWriter, queries *db.Queries, identity authctx.Identity, spaceID uuid.UUID, min Role) bool {
	ok, err := HasSpaceRole(ctx, queries, identity, spaceID, min)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to check permissions")
		return false
	}
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "insufficient space role")
		return false
	}
	return true
}
