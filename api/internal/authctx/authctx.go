// Package authctx carries the authenticated (or guest) identity for a
// request through context.Context, and provides the authorization helpers
// handlers use to enforce SPEC.md §6.5's space-level roles.
package authctx

import (
	"context"

	"github.com/google/uuid"
)

// Identity is exactly one of a real user or a guest identity, mirroring the
// `sessions` table's CHECK constraint (SPEC.md §6.4, Decision 6).
type Identity struct {
	UserID          *uuid.UUID
	GuestIdentityID *uuid.UUID
	DisplayName     string
	// IsOrgAdmin mirrors the users.is_org_admin column for real users. An
	// org admin is treated as authorized for every space-level check below
	// — SPEC.md doesn't say this explicitly, but it's the only sane reading
	// of "org-level admin manages users/spaces/instance settings" (§6.5)
	// short of seeding a space_members row for every space on every org
	// admin, which would be a strange source of truth to maintain.
	IsOrgAdmin bool

	// ShareLinkPageID/SharePermission are set only for a guest identity
	// created by redeeming a share link — see internal/sessiontoken. They
	// come from the live share_links row (checked not revoked/expired on
	// every request by Middleware), never from the token itself. nil/"" for
	// every other identity, including unscoped guests (there currently
	// aren't any: guest identities are only ever created via share-link
	// redemption, per SPEC.md §6.6).
	ShareLinkPageID *uuid.UUID
	SharePermission string
}

func (i Identity) IsGuest() bool {
	return i.GuestIdentityID != nil
}

type contextKey struct{}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the request's identity. Panics if called on a request
// that didn't go through the auth middleware — that's a routing bug, not a
// runtime condition to handle gracefully.
func FromContext(ctx context.Context) Identity {
	id, ok := ctx.Value(contextKey{}).(Identity)
	if !ok {
		panic("authctx: no identity in context — handler mounted outside the auth middleware")
	}
	return id
}
