package spacesapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authz"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
)

// errLastOwner is returned when a change would leave a space with no
// explicit owner.
var errLastOwner = errors.New("a space must keep at least one owner")

// mutateMembers runs fn in a transaction holding a lock on the space row,
// so the "last owner" check and the change it guards are atomic against
// concurrent member changes. fn returns the HTTP status/body to write; it is
// committed only when status < 400.
func (h *Handler) mutateMembers(w http.ResponseWriter, r *http.Request, spaceID uuid.UUID, fn func(q *db.Queries) (int, any)) {
	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.logger.Error("member change: begin tx failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to change members")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := h.queries.WithTx(tx)
	if _, err := q.LockSpace(ctx, db.ToPgUUID(spaceID)); err != nil {
		httpx.WriteLookupError(w, h.logger, err, "space not found")
		return
	}
	status, body := fn(q)
	if status >= 400 {
		if msg, ok := body.(string); ok {
			httpx.WriteError(w, status, msg)
		} else {
			httpx.WriteJSON(w, status, body)
		}
		return
	}
	if err := tx.Commit(ctx); err != nil {
		h.logger.Error("member change: commit failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to change members")
		return
	}
	if body == nil {
		w.WriteHeader(status)
		return
	}
	httpx.WriteJSON(w, status, body)
}

// ownerStays reports whether the space keeps a direct (user) owner if the
// user row currently holding `existing` (nil: no such row) changes to `next`
// (nil: removed). Group rows are never guarded: group membership follows the
// IdP, so a group can't be what keeps a space owned (CountSpaceOwners).
func ownerStays(ctx context.Context, q *db.Queries, spaceID pgtype.UUID, existing, next *db.SpaceRole) (bool, error) {
	if existing == nil || *existing != db.SpaceRoleOwner || (next != nil && *next == db.SpaceRoleOwner) {
		return true, nil
	}
	n, err := q.CountSpaceOwners(ctx, spaceID)
	if err != nil {
		return false, err
	}
	return n > 1, nil
}

// touchesOwner reports whether a change involves the owner role, either as
// the role being replaced or the role being granted.
func touchesOwner(existing, next *db.SpaceRole) bool {
	return (existing != nil && *existing == db.SpaceRoleOwner) || (next != nil && *next == db.SpaceRoleOwner)
}

// callerMayChangeOwners: granting, changing or revoking the owner role needs
// an owner (org admins pass) — an admin could otherwise promote themselves to
// owner and then delete the space (Decision 46).
func (h *Handler) callerMayChangeOwners(r *http.Request, spaceID uuid.UUID) (bool, error) {
	return authz.HasSpaceRole(r.Context(), h.queries, authctx.FromContext(r.Context()), spaceID, authz.RoleOwner)
}

const errOwnerOnly = "only a space owner can grant, change or remove the owner role"

// ownerGate returns a non-zero status when the change touches the owner role
// and the caller isn't an owner.
func (h *Handler) ownerGate(r *http.Request, spaceID uuid.UUID, existing, next *db.SpaceRole) (int, string) {
	if !touchesOwner(existing, next) {
		return 0, ""
	}
	ok, err := h.callerMayChangeOwners(r, spaceID)
	if err != nil {
		h.logger.Error("checking owner permission failed", "error", err)
		return http.StatusInternalServerError, "failed to check permissions"
	}
	if !ok {
		return http.StatusForbidden, errOwnerOnly
	}
	return 0, ""
}

func (h *Handler) requireAdminParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return uuid.Nil, false
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, spaceID, authz.RoleAdmin) {
		return uuid.Nil, false
	}
	return spaceID, true
}

type addMemberRequest struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := h.requireAdminParam(w, r)
	if !ok {
		return
	}
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid userId")
		return
	}
	role, ok := parseSpaceRole(req.Role)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "role must be one of owner, admin, editor, viewer")
		return
	}
	sid := db.ToPgUUID(spaceID)
	h.mutateMembers(w, r, spaceID, func(q *db.Queries) (int, any) {
		// Adding an existing member is an upsert, so it can demote an owner.
		var existing *db.SpaceRole
		if m, err := q.GetSpaceMemberUser(r.Context(), db.GetSpaceMemberUserParams{SpaceID: sid, UserID: db.ToPgUUID(userID)}); err == nil {
			existing = &m.Role
		} else if !errors.Is(err, pgx.ErrNoRows) {
			h.logger.Error("add space member: lookup failed", "error", err)
			return http.StatusInternalServerError, "failed to add member"
		}
		if status, msg := h.ownerGate(r, spaceID, existing, &role); status != 0 {
			return status, msg
		}
		if stays, err := ownerStays(r.Context(), q, sid, existing, &role); err != nil {
			h.logger.Error("add space member: owner count failed", "error", err)
			return http.StatusInternalServerError, "failed to add member"
		} else if !stays {
			return http.StatusConflict, errLastOwner.Error()
		}
		member, err := q.AddSpaceMemberUser(r.Context(), db.AddSpaceMemberUserParams{SpaceID: sid, UserID: db.ToPgUUID(userID), Role: role})
		if err != nil {
			if db.IsForeignKeyViolation(err) {
				return http.StatusNotFound, "space or user not found"
			}
			h.logger.Error("add space member failed", "error", err)
			return http.StatusInternalServerError, "failed to add member"
		}
		return http.StatusCreated, toSpaceMemberResponse(member)
	})
}

type updateMemberRequest struct {
	Role string `json:"role"`
}

func (h *Handler) updateMember(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := h.requireAdminParam(w, r)
	if !ok {
		return
	}
	memberUserID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req updateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	role, ok := parseSpaceRole(req.Role)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "role must be one of owner, admin, editor, viewer")
		return
	}
	sid := db.ToPgUUID(spaceID)
	h.mutateMembers(w, r, spaceID, func(q *db.Queries) (int, any) {
		cur, err := q.GetSpaceMemberUser(r.Context(), db.GetSpaceMemberUserParams{SpaceID: sid, UserID: db.ToPgUUID(memberUserID)})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return http.StatusNotFound, "user is not a member of this space"
			}
			h.logger.Error("update space member: lookup failed", "error", err)
			return http.StatusInternalServerError, "failed to update member"
		}
		if status, msg := h.ownerGate(r, spaceID, &cur.Role, &role); status != 0 {
			return status, msg
		}
		if stays, err := ownerStays(r.Context(), q, sid, &cur.Role, &role); err != nil {
			h.logger.Error("update space member: owner count failed", "error", err)
			return http.StatusInternalServerError, "failed to update member"
		} else if !stays {
			return http.StatusConflict, errLastOwner.Error()
		}
		member, err := q.UpdateSpaceMemberRole(r.Context(), db.UpdateSpaceMemberRoleParams{SpaceID: sid, UserID: db.ToPgUUID(memberUserID), Role: role})
		if err != nil {
			h.logger.Error("update space member role failed", "error", err)
			return http.StatusInternalServerError, "failed to update member"
		}
		return http.StatusOK, toSpaceMemberResponse(member)
	})
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := h.requireAdminParam(w, r)
	if !ok {
		return
	}
	memberUserID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	sid := db.ToPgUUID(spaceID)
	h.mutateMembers(w, r, spaceID, func(q *db.Queries) (int, any) {
		cur, err := q.GetSpaceMemberUser(r.Context(), db.GetSpaceMemberUserParams{SpaceID: sid, UserID: db.ToPgUUID(memberUserID)})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return http.StatusNoContent, nil // already gone: idempotent
			}
			h.logger.Error("remove space member: lookup failed", "error", err)
			return http.StatusInternalServerError, "failed to remove member"
		}
		if status, msg := h.ownerGate(r, spaceID, &cur.Role, nil); status != 0 {
			return status, msg
		}
		if stays, err := ownerStays(r.Context(), q, sid, &cur.Role, nil); err != nil {
			h.logger.Error("remove space member: owner count failed", "error", err)
			return http.StatusInternalServerError, "failed to remove member"
		} else if !stays {
			return http.StatusConflict, errLastOwner.Error()
		}
		if _, err := q.RemoveSpaceMemberUser(r.Context(), db.RemoveSpaceMemberUserParams{SpaceID: sid, UserID: db.ToPgUUID(memberUserID)}); err != nil {
			h.logger.Error("remove space member failed", "error", err)
			return http.StatusInternalServerError, "failed to remove member"
		}
		return http.StatusNoContent, nil
	})
}

// --- group principals (SPEC.md §3.1 Group) -------------------------------

type addGroupMemberRequest struct {
	GroupID string `json:"groupId"`
	Role    string `json:"role"`
}

func (h *Handler) addGroupMember(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := h.requireAdminParam(w, r)
	if !ok {
		return
	}
	var req addGroupMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	groupID, err := uuid.Parse(req.GroupID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid groupId")
		return
	}
	role, ok := parseSpaceRole(req.Role)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "role must be one of owner, admin, editor, viewer")
		return
	}
	sid := db.ToPgUUID(spaceID)
	h.mutateMembers(w, r, spaceID, func(q *db.Queries) (int, any) {
		var existingGroup *db.SpaceRole
		if m, err := q.GetSpaceMemberGroup(r.Context(), db.GetSpaceMemberGroupParams{SpaceID: sid, GroupID: db.ToPgUUID(groupID)}); err == nil {
			existingGroup = &m.Role
		} else if !errors.Is(err, pgx.ErrNoRows) {
			h.logger.Error("add group member: lookup failed", "error", err)
			return http.StatusInternalServerError, "failed to add group"
		}
		if status, msg := h.ownerGate(r, spaceID, existingGroup, &role); status != 0 {
			return status, msg
		}
		member, err := q.AddSpaceMemberGroup(r.Context(), db.AddSpaceMemberGroupParams{SpaceID: sid, GroupID: db.ToPgUUID(groupID), Role: role})
		if err != nil {
			if db.IsForeignKeyViolation(err) {
				return http.StatusNotFound, "space or group not found"
			}
			h.logger.Error("add group member failed", "error", err)
			return http.StatusInternalServerError, "failed to add group"
		}
		return http.StatusCreated, toSpaceMemberResponse(member)
	})
}

func (h *Handler) updateGroupMember(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := h.requireAdminParam(w, r)
	if !ok {
		return
	}
	groupID, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	var req updateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	role, ok := parseSpaceRole(req.Role)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "role must be one of owner, admin, editor, viewer")
		return
	}
	sid := db.ToPgUUID(spaceID)
	h.mutateMembers(w, r, spaceID, func(q *db.Queries) (int, any) {
		curGroup, err := q.GetSpaceMemberGroup(r.Context(), db.GetSpaceMemberGroupParams{SpaceID: sid, GroupID: db.ToPgUUID(groupID)})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return http.StatusNotFound, "group is not a member of this space"
			}
			h.logger.Error("update group member: lookup failed", "error", err)
			return http.StatusInternalServerError, "failed to update group"
		}
		if status, msg := h.ownerGate(r, spaceID, &curGroup.Role, &role); status != 0 {
			return status, msg
		}
		member, err := q.UpdateSpaceMemberGroupRole(r.Context(), db.UpdateSpaceMemberGroupRoleParams{SpaceID: sid, GroupID: db.ToPgUUID(groupID), Role: role})
		if err != nil {
			h.logger.Error("update group member role failed", "error", err)
			return http.StatusInternalServerError, "failed to update group"
		}
		return http.StatusOK, toSpaceMemberResponse(member)
	})
}

func (h *Handler) removeGroupMember(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := h.requireAdminParam(w, r)
	if !ok {
		return
	}
	groupID, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	sid := db.ToPgUUID(spaceID)
	h.mutateMembers(w, r, spaceID, func(q *db.Queries) (int, any) {
		curGroup, err := q.GetSpaceMemberGroup(r.Context(), db.GetSpaceMemberGroupParams{SpaceID: sid, GroupID: db.ToPgUUID(groupID)})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return http.StatusNoContent, nil
			}
			h.logger.Error("remove group member: lookup failed", "error", err)
			return http.StatusInternalServerError, "failed to remove group"
		}
		if status, msg := h.ownerGate(r, spaceID, &curGroup.Role, nil); status != 0 {
			return status, msg
		}
		if _, err := q.RemoveSpaceMemberGroup(r.Context(), db.RemoveSpaceMemberGroupParams{SpaceID: sid, GroupID: db.ToPgUUID(groupID)}); err != nil {
			h.logger.Error("remove group member failed", "error", err)
			return http.StatusInternalServerError, "failed to remove group"
		}
		return http.StatusNoContent, nil
	})
}

// --- pickers for space admins (who are not necessarily org admins) --------

const minCandidateQuery = 2

type memberCandidateResponse struct {
	ID          pgtype.UUID `json:"id"`
	Email       string      `json:"email"`
	DisplayName string      `json:"display_name"`
}

// memberCandidates lets a space admin find enabled users to add without
// needing org-admin access to GET /users. A query of at least two characters
// is required so it can't be used to dump the directory.
func (h *Handler) memberCandidates(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := h.requireAdminParam(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < minCandidateQuery {
		httpx.WriteError(w, http.StatusBadRequest, "q must be at least 2 characters")
		return
	}
	rows, err := h.queries.SearchMemberCandidates(r.Context(), db.SearchMemberCandidatesParams{Q: httpx.EscapeLike(q), SpaceID: db.ToPgUUID(spaceID), Limit: 20})
	if err != nil {
		h.logger.Error("member candidates failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to search users")
		return
	}
	out := make([]memberCandidateResponse, len(rows))
	for i, u := range rows {
		out[i] = memberCandidateResponse{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type groupCandidateResponse struct {
	ID   pgtype.UUID `json:"id"`
	Name string      `json:"name"`
}

// groupCandidates lists known groups (created from the IdP groups claim at
// OIDC login) for the space admin's picker; q is optional.
func (h *Handler) groupCandidates(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdminParam(w, r); !ok {
		return
	}
	params := db.ListGroupsParams{Limit: 50}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		esc := httpx.EscapeLike(q)
		params.Q = &esc
	}
	rows, err := h.queries.ListGroups(r.Context(), params)
	if err != nil {
		h.logger.Error("list groups failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}
	out := make([]groupCandidateResponse, len(rows))
	for i, g := range rows {
		out[i] = groupCandidateResponse{ID: g.ID, Name: g.Name}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
