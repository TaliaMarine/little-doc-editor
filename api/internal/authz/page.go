package authz

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
)

// CanAccessPage reports whether identity may access pageID at least at min
// role/permission. For a real user this is exactly their space role. For a
// share-link guest (SPEC.md §6.6), it's whether pageID is the guest's
// granted share-link page or one of its descendants (Decision 21), and
// whether the link's permission (view/edit) covers what min requires.
func CanAccessPage(ctx context.Context, queries *db.Queries, identity authctx.Identity, pageSpaceID, pageID uuid.UUID, min Role) (bool, error) {
	if identity.IsOrgAdmin {
		return true, nil
	}

	if identity.UserID != nil {
		return HasSpaceRole(ctx, queries, identity, pageSpaceID, min)
	}

	if identity.ShareLinkPageID == nil {
		return false, nil
	}
	// Guests never hold admin/owner-level rights, whatever the link says.
	if min > RoleEditor {
		return false, nil
	}
	// A view-permission link never covers an edit-requiring action.
	if min >= RoleEditor && identity.SharePermission != "edit" {
		return false, nil
	}

	if *identity.ShareLinkPageID == pageID {
		return true, nil
	}
	descendants, err := queries.ListPageDescendantIDs(ctx, db.ToPgUUID(*identity.ShareLinkPageID))
	if err != nil {
		return false, err
	}
	for _, d := range descendants {
		if db.FromPgUUID(d) == pageID {
			return true, nil
		}
	}
	return false, nil
}

// PageCapabilities is what identity may do with one page, for both
// authorization and reporting to the UI. It agrees with CanAccessPage by
// construction (same rules), but resolves view and edit in one pass —
// one role lookup, or one descendant walk for a share-link guest.
type PageCapabilities struct {
	// Role is the caller's effective space role (see EffectiveSpaceRole);
	// nil for guests and non-members.
	Role    *db.SpaceRole
	CanView bool
	// CanEdit covers content writes and attachment uploads. It does not
	// account for pages.locked, which applies to everyone and is reported
	// separately.
	CanEdit bool
}

// CapabilitiesForSpaceRole is what an effective space role (from
// EffectiveSpaceRole) grants on any page in that space. For handlers whose
// authorization is space-level (create, move), so they can authorize and
// fill my_role/can_edit from a single role lookup.
func CapabilitiesForSpaceRole(role *db.SpaceRole) PageCapabilities {
	if role == nil {
		return PageCapabilities{}
	}
	r, ok := parseRole(*role)
	if !ok {
		return PageCapabilities{}
	}
	return PageCapabilities{Role: role, CanView: true, CanEdit: r.MeetsMinimum(RoleEditor)}
}

func ResolvePageCapabilities(ctx context.Context, queries *db.Queries, identity authctx.Identity, pageSpaceID, pageID uuid.UUID) (PageCapabilities, error) {
	if identity.UserID != nil || identity.IsOrgAdmin {
		role, err := EffectiveSpaceRole(ctx, queries, identity, pageSpaceID)
		if err != nil {
			return PageCapabilities{}, err
		}
		return CapabilitiesForSpaceRole(role), nil
	}

	inScope, err := CanAccessPage(ctx, queries, identity, pageSpaceID, pageID, RoleViewer)
	if err != nil || !inScope {
		return PageCapabilities{}, err
	}
	return PageCapabilities{CanView: true, CanEdit: identity.SharePermission == "edit"}, nil
}

func RequirePageAccess(ctx context.Context, w http.ResponseWriter, queries *db.Queries, identity authctx.Identity, pageSpaceID, pageID uuid.UUID, min Role) bool {
	ok, err := CanAccessPage(ctx, queries, identity, pageSpaceID, pageID, min)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to check permissions")
		return false
	}
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "insufficient access to this page")
		return false
	}
	return true
}
