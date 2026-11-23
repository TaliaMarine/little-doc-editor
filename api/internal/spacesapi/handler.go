// Package spacesapi implements space CRUD and member/role management
// (SPEC.md §6.1, §6.5, Decisions 7, 24). Creating a space still requires
// org-admin (space creation itself isn't a member-management action to
// delegate); the creator is added as the space's first owner so they aren't
// locked out of their own space afterward.
package spacesapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authz"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
)

type Handler struct {
	queries *db.Queries
	logger  *slog.Logger
}

func NewHandler(queries *db.Queries, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, logger: logger}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/spaces", h.create)
	r.Get("/spaces", h.list)
	r.Get("/spaces/{spaceID}", h.get)

	r.Get("/spaces/{spaceID}/members", h.listMembers)
	r.Post("/spaces/{spaceID}/members", h.addMember)
	r.Patch("/spaces/{spaceID}/members/{userID}", h.updateMember)
	r.Delete("/spaces/{spaceID}/members/{userID}", h.removeMember)
}

// spaceResponse, spaceMemberResponse, and spaceMemberListItemResponse are
// explicit API shapes rather than the raw sqlc rows, so a future
// internal-only column on spaces/space_members can't silently leak into the
// public contract (same pattern as pagesapi's pageResponse).
type spaceResponse struct {
	ID                pgtype.UUID        `json:"id"`
	Name              string             `json:"name"`
	Slug              string             `json:"slug"`
	Icon              *string            `json:"icon"`
	HistoryMaxCount   *int32             `json:"history_max_count"`
	HistoryMaxAgeDays *int32             `json:"history_max_age_days"`
	CreatedAt         pgtype.Timestamptz `json:"created_at"`
	UpdatedAt         pgtype.Timestamptz `json:"updated_at"`
	// MyRole is the caller's effective role in this space
	// (authz.EffectiveSpaceRole) — UX gating only.
	// Non-pointer: every space the caller can see has a role for them
	// (openapi.yaml marks it required and non-null).
	MyRole db.SpaceRole `json:"my_role"`
}

func toSpaceResponse(sp db.Space, myRole db.SpaceRole) spaceResponse {
	return spaceResponse{
		ID:                sp.ID,
		Name:              sp.Name,
		Slug:              sp.Slug,
		Icon:              sp.Icon,
		HistoryMaxCount:   sp.HistoryMaxCount,
		HistoryMaxAgeDays: sp.HistoryMaxAgeDays,
		CreatedAt:         sp.CreatedAt,
		UpdatedAt:         sp.UpdatedAt,
		MyRole:            myRole,
	}
}

type spaceMemberResponse struct {
	ID        pgtype.UUID        `json:"id"`
	SpaceID   pgtype.UUID        `json:"space_id"`
	UserID    pgtype.UUID        `json:"user_id"`
	GroupID   pgtype.UUID        `json:"group_id"`
	Role      db.SpaceRole       `json:"role"`
	CreatedAt pgtype.Timestamptz `json:"created_at"`
}

func toSpaceMemberResponse(m db.SpaceMember) spaceMemberResponse {
	return spaceMemberResponse{
		ID:        m.ID,
		SpaceID:   m.SpaceID,
		UserID:    m.UserID,
		GroupID:   m.GroupID,
		Role:      m.Role,
		CreatedAt: m.CreatedAt,
	}
}

type spaceMemberListItemResponse struct {
	spaceMemberResponse
	UserEmail       *string `json:"user_email"`
	UserDisplayName *string `json:"user_display_name"`
}

type createSpaceRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
	Icon string `json:"icon"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	identity := authctx.FromContext(r.Context())
	if !identity.IsOrgAdmin {
		httpx.WriteError(w, http.StatusForbidden, "only org admins can create spaces")
		return
	}

	var req createSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Name == "" || req.Slug == "" {
		httpx.WriteError(w, http.StatusBadRequest, "name and slug are required")
		return
	}

	space, err := h.queries.CreateSpace(r.Context(), db.CreateSpaceParams{
		Name: req.Name,
		Slug: req.Slug,
		Icon: httpx.PtrOrNil(req.Icon),
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			httpx.WriteError(w, http.StatusConflict, "a space with this slug already exists")
			return
		}
		h.logger.Error("create space failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create space")
		return
	}

	// The creator becomes the space's first owner — otherwise an org admin
	// who isn't also a member couldn't be distinguished from any other org
	// admin for this space's own member list, and a non-admin creator (once
	// space creation opens up beyond org-admins-only) would create a space
	// they then have no explicit role in.
	if identity.UserID != nil {
		if _, err := h.queries.AddSpaceMemberUser(r.Context(), db.AddSpaceMemberUserParams{
			SpaceID: space.ID,
			UserID:  db.ToPgUUID(*identity.UserID),
			Role:    db.SpaceRoleOwner,
		}); err != nil {
			h.logger.Error("adding creator as space owner failed", "error", err)
		}
	}

	// The creator was just made owner above; an org admin is reported as
	// owner regardless (authz.EffectiveSpaceRole).
	httpx.WriteJSON(w, http.StatusCreated, toSpaceResponse(space, db.SpaceRoleOwner))
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, spaceID, authz.RoleViewer) {
		return
	}

	members, err := h.queries.ListSpaceMembers(r.Context(), db.ToPgUUID(spaceID))
	if err != nil {
		h.logger.Error("list space members failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	items := make([]spaceMemberListItemResponse, len(members))
	for i, m := range members {
		items[i] = spaceMemberListItemResponse{
			spaceMemberResponse: spaceMemberResponse{
				ID:        m.ID,
				SpaceID:   m.SpaceID,
				UserID:    m.UserID,
				GroupID:   m.GroupID,
				Role:      m.Role,
				CreatedAt: m.CreatedAt,
			},
			UserEmail:       m.UserEmail,
			UserDisplayName: m.UserDisplayName,
		}
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}

type addMemberRequest struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, spaceID, authz.RoleAdmin) {
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

	member, err := h.queries.AddSpaceMemberUser(r.Context(), db.AddSpaceMemberUserParams{
		SpaceID: db.ToPgUUID(spaceID),
		UserID:  db.ToPgUUID(userID),
		Role:    role,
	})
	if err != nil {
		if db.IsForeignKeyViolation(err) {
			httpx.WriteError(w, http.StatusNotFound, "space or user not found")
			return
		}
		h.logger.Error("add space member failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toSpaceMemberResponse(member))
}

type updateMemberRequest struct {
	Role string `json:"role"`
}

func (h *Handler) updateMember(w http.ResponseWriter, r *http.Request) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}
	memberUserID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, spaceID, authz.RoleAdmin) {
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

	member, err := h.queries.UpdateSpaceMemberRole(r.Context(), db.UpdateSpaceMemberRoleParams{
		SpaceID: db.ToPgUUID(spaceID),
		UserID:  db.ToPgUUID(memberUserID),
		Role:    role,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteError(w, http.StatusNotFound, "user is not a member of this space")
			return
		}
		h.logger.Error("update space member role failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update member")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toSpaceMemberResponse(member))
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}
	memberUserID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, spaceID, authz.RoleAdmin) {
		return
	}

	if err := h.queries.RemoveSpaceMemberUser(r.Context(), db.RemoveSpaceMemberUserParams{
		SpaceID: db.ToPgUUID(spaceID),
		UserID:  db.ToPgUUID(memberUserID),
	}); err != nil {
		h.logger.Error("remove space member failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseSpaceRole(s string) (db.SpaceRole, bool) {
	switch db.SpaceRole(s) {
	case db.SpaceRoleOwner, db.SpaceRoleAdmin, db.SpaceRoleEditor, db.SpaceRoleViewer:
		return db.SpaceRole(s), true
	default:
		return "", false
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	identity := authctx.FromContext(r.Context())

	if identity.IsOrgAdmin {
		spaces, err := h.queries.ListAllSpaces(r.Context())
		if err != nil {
			h.logger.Error("list spaces failed", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "failed to list spaces")
			return
		}
		out := make([]spaceResponse, len(spaces))
		for i, sp := range spaces {
			out[i] = toSpaceResponse(sp, db.SpaceRoleOwner)
		}
		httpx.WriteJSON(w, http.StatusOK, out)
		return
	}

	if identity.UserID == nil {
		httpx.WriteJSON(w, http.StatusOK, []spaceResponse{})
		return
	}
	rows, err := h.queries.ListSpacesForUser(r.Context(), db.ToPgUUID(*identity.UserID))
	if err != nil {
		h.logger.Error("list spaces failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list spaces")
		return
	}
	out := make([]spaceResponse, len(rows))
	for i, row := range rows {
		out[i] = toSpaceResponse(db.Space{
			ID:                row.ID,
			Name:              row.Name,
			Slug:              row.Slug,
			Icon:              row.Icon,
			HistoryMaxCount:   row.HistoryMaxCount,
			HistoryMaxAgeDays: row.HistoryMaxAgeDays,
			CreatedAt:         row.CreatedAt,
			UpdatedAt:         row.UpdatedAt,
		}, row.Role)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}

	space, err := h.queries.GetSpaceByID(r.Context(), db.ToPgUUID(id))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "space not found")
		return
	}

	identity := authctx.FromContext(r.Context())
	myRole, err := authz.EffectiveSpaceRole(r.Context(), h.queries, identity, id)
	if err != nil {
		h.logger.Error("resolving space role failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to check permissions")
		return
	}
	if myRole == nil {
		httpx.WriteError(w, http.StatusForbidden, "insufficient space role")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toSpaceResponse(space, *myRole))
}
