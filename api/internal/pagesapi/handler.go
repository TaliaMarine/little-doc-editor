// Package pagesapi implements page CRUD, tree reordering, and search
// indexing (SPEC.md §5, §6.2, §6.8). Content is stored as ProseMirror/
// Tiptap JSON (Decision 4) — this layer treats it as an opaque JSON blob,
// since validating editor-schema shape belongs to the frontend/editor, not
// the API.
package pagesapi

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
	"github.com/TaliaMarine/little-doc-editor/api/internal/searchindex"
)

type Handler struct {
	queries *db.Queries
	indexer *searchindex.Indexer
	logger  *slog.Logger
}

func NewHandler(queries *db.Queries, indexer *searchindex.Indexer, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, indexer: indexer, logger: logger}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/spaces/{spaceID}/pages", h.create)
	r.Get("/spaces/{spaceID}/pages", h.list)
	r.Get("/pages/{pageID}", h.get)
	r.Put("/pages/{pageID}/content", h.updateContent)
	r.Patch("/pages/{pageID}/move", h.move)
}

type createPageRequest struct {
	ParentID *string         `json:"parentId"`
	Title    string          `json:"title"`
	Content  json.RawMessage `json:"content"`
}

// pageResponse mirrors db.Page for API responses, except Content is
// json.RawMessage instead of []byte — encoding/json base64-encodes a bare
// []byte field, which would silently turn the page's ProseMirror/Tiptap
// JSON (Decision 4) into an opaque base64 blob for every API consumer.
type pageResponse struct {
	ID               pgtype.UUID        `json:"id"`
	SpaceID          pgtype.UUID        `json:"space_id"`
	ParentID         pgtype.UUID        `json:"parent_id"`
	Position         float64            `json:"position"`
	Title            string             `json:"title"`
	Content          json.RawMessage    `json:"content"`
	HistoryEnabled   *bool              `json:"history_enabled"`
	Locked           bool               `json:"locked"`
	IsTemplate       bool               `json:"is_template"`
	TemplateKey      *string            `json:"template_key"`
	CreatedByUserID  pgtype.UUID        `json:"created_by_user_id"`
	CreatedByGuestID pgtype.UUID        `json:"created_by_guest_id"`
	CreatedAt        pgtype.Timestamptz `json:"created_at"`
	UpdatedAt        pgtype.Timestamptz `json:"updated_at"`

	// MyRole/CanEdit describe the *caller's* access (authz.PageCapabilities)
	// so the UI can hide what the server would refuse. UX only — every
	// write is still authorized server-side.
	MyRole  *db.SpaceRole `json:"my_role"`
	CanEdit bool          `json:"can_edit"`
}

func toPageResponse(p db.Page, caps authz.PageCapabilities) pageResponse {
	return pageResponse{
		ID:               p.ID,
		SpaceID:          p.SpaceID,
		ParentID:         p.ParentID,
		Position:         p.Position,
		Title:            p.Title,
		Content:          json.RawMessage(p.Content),
		HistoryEnabled:   p.HistoryEnabled,
		Locked:           p.Locked,
		IsTemplate:       p.IsTemplate,
		TemplateKey:      p.TemplateKey,
		CreatedByUserID:  p.CreatedByUserID,
		CreatedByGuestID: p.CreatedByGuestID,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
		MyRole:           caps.Role,
		CanEdit:          caps.CanEdit,
	}
}

// Every handler that returns a page resolves the caller's capabilities
// exactly once, *before* any write, and uses that one result both to
// authorize and to fill my_role/can_edit — so a response can never fail
// (or cost another lookup) after the write already committed.

// requireSpaceEditor authorizes space-level page actions (create, move —
// member-only, never share-link guests) and returns the capabilities to
// report on the resulting page.
func (h *Handler) requireSpaceEditor(w http.ResponseWriter, r *http.Request, spaceID uuid.UUID) (authz.PageCapabilities, bool) {
	identity := authctx.FromContext(r.Context())
	role, err := authz.EffectiveSpaceRole(r.Context(), h.queries, identity, spaceID)
	if err != nil {
		h.logger.Error("resolving space role failed", "error", err, "space_id", spaceID)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to check permissions")
		return authz.PageCapabilities{}, false
	}
	caps := authz.CapabilitiesForSpaceRole(role)
	if !caps.CanEdit {
		httpx.WriteError(w, http.StatusForbidden, "insufficient space role")
		return authz.PageCapabilities{}, false
	}
	return caps, true
}

// resolvePage resolves the caller's capabilities on one page, writing a
// 500 on lookup failure.
func (h *Handler) resolvePage(w http.ResponseWriter, r *http.Request, page db.Page) (authz.PageCapabilities, bool) {
	identity := authctx.FromContext(r.Context())
	caps, err := authz.ResolvePageCapabilities(r.Context(), h.queries, identity, db.FromPgUUID(page.SpaceID), db.FromPgUUID(page.ID))
	if err != nil {
		h.logger.Error("resolving page capabilities failed", "error", err, "page_id", db.FromPgUUID(page.ID))
		httpx.WriteError(w, http.StatusInternalServerError, "failed to check permissions")
		return authz.PageCapabilities{}, false
	}
	return caps, true
}

// pageTreeItemResponse is the shape returned by list — the sidebar tree nav
// needs every page in a space to build parent/child structure, but not each
// page's full content (Decision 4's ProseMirror/Tiptap JSON can be large,
// and the nav has no use for it).
type pageTreeItemResponse struct {
	ID         pgtype.UUID        `json:"id"`
	ParentID   pgtype.UUID        `json:"parent_id"`
	Position   float64            `json:"position"`
	Title      string             `json:"title"`
	Locked     bool               `json:"locked"`
	IsTemplate bool               `json:"is_template"`
	CreatedAt  pgtype.Timestamptz `json:"created_at"`
	UpdatedAt  pgtype.Timestamptz `json:"updated_at"`
}

func toPageTreeItemResponse(p db.ListPagesBySpaceRow) pageTreeItemResponse {
	return pageTreeItemResponse{
		ID:         p.ID,
		ParentID:   p.ParentID,
		Position:   p.Position,
		Title:      p.Title,
		Locked:     p.Locked,
		IsTemplate: p.IsTemplate,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
}

// list backs the page-tree sidebar nav (SPEC.md §5) — the whole space's
// pages flattened, ordered by position; the frontend reconstructs the tree
// from parent_id client-side.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}
	if _, err := h.queries.GetSpaceByID(r.Context(), db.ToPgUUID(spaceID)); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "space not found")
		return
	}

	identity := authctx.FromContext(r.Context())
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, identity, spaceID, authz.RoleViewer) {
		return
	}

	pages, err := h.queries.ListPagesBySpace(r.Context(), db.ToPgUUID(spaceID))
	if err != nil {
		h.logger.Error("list pages failed", "error", err, "space_id", spaceID)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list pages")
		return
	}

	items := make([]pageTreeItemResponse, len(pages))
	for i, p := range pages {
		items[i] = toPageTreeItemResponse(p)
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}
	if _, err := h.queries.GetSpaceByID(r.Context(), db.ToPgUUID(spaceID)); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "space not found")
		return
	}

	identity := authctx.FromContext(r.Context())
	caps, ok := h.requireSpaceEditor(w, r, spaceID)
	if !ok {
		return
	}

	var req createPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Title == "" {
		httpx.WriteError(w, http.StatusBadRequest, "title is required")
		return
	}
	content := req.Content
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}

	params := db.CreatePageParams{
		SpaceID: db.ToPgUUID(spaceID),
		Title:   req.Title,
		Content: content,
	}
	if req.ParentID != nil {
		parentID, ok := h.parseParentInSpace(w, r, *req.ParentID, spaceID)
		if !ok {
			return
		}
		params.ParentID = db.ToPgUUID(parentID)
	}
	if identity.UserID != nil {
		params.CreatedByUserID = db.ToPgUUID(*identity.UserID)
	}

	// Append after existing siblings rather than leaving every page at the
	// position column's zero default — otherwise ListPagesBySpace's
	// ordering (the sidebar tree nav, SPEC.md §5) is meaningless once a
	// space has more than one page per level.
	var maxPosition float64
	if params.ParentID.Valid {
		maxPosition, err = h.queries.GetMaxChildPagePosition(r.Context(), db.GetMaxChildPagePositionParams{
			SpaceID:  params.SpaceID,
			ParentID: params.ParentID,
		})
	} else {
		maxPosition, err = h.queries.GetMaxRootPagePosition(r.Context(), params.SpaceID)
	}
	if err != nil {
		h.logger.Error("computing new page position failed", "error", err, "space_id", spaceID)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create page")
		return
	}
	params.Position = maxPosition + 1

	page, err := h.queries.CreatePage(r.Context(), params)
	if err != nil {
		h.logger.Error("create page failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create page")
		return
	}

	if err := h.indexer.IndexPage(r.Context(), db.FromPgUUID(page.ID), page.Content); err != nil {
		h.logger.Error("indexing new page failed", "error", err)
	}

	httpx.WriteJSON(w, http.StatusCreated, toPageResponse(page, caps))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}

	page, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(id))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "page not found")
		return
	}

	caps, ok := h.resolvePage(w, r, page)
	if !ok {
		return
	}
	if !caps.CanView {
		httpx.WriteError(w, http.StatusForbidden, "insufficient access to this page")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toPageResponse(page, caps))
}

type updateContentRequest struct {
	Title   string          `json:"title"`
	Content json.RawMessage `json:"content"`
}

func (h *Handler) updateContent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}
	existing, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(id))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "page not found")
		return
	}

	caps, ok := h.resolvePage(w, r, existing)
	if !ok {
		return
	}
	if !caps.CanEdit {
		httpx.WriteError(w, http.StatusForbidden, "insufficient access to this page")
		return
	}
	if existing.Locked {
		httpx.WriteError(w, http.StatusForbidden, "page is locked")
		return
	}

	var req updateContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Title == "" {
		httpx.WriteError(w, http.StatusBadRequest, "title is required")
		return
	}
	content := req.Content
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}

	page, err := h.queries.UpdatePageContent(r.Context(), db.UpdatePageContentParams{
		ID:      db.ToPgUUID(id),
		Content: content,
		Title:   req.Title,
	})
	if err != nil {
		h.logger.Error("update page content failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update page")
		return
	}

	if err := h.indexer.IndexPage(r.Context(), id, page.Content); err != nil {
		h.logger.Error("indexing updated page failed", "error", err)
	}

	httpx.WriteJSON(w, http.StatusOK, toPageResponse(page, caps))
}

type moveRequest struct {
	ParentID *string `json:"parentId"`
	Position float64 `json:"position"`
}

// move backs page-tree drag-and-drop reordering/reparenting (SPEC.md §5).
// The frontend computes Position as the midpoint between the two flanking
// siblings in the destination location; this endpoint just persists it.
func (h *Handler) move(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}
	existing, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(id))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "page not found")
		return
	}

	// Restructuring the tree is a space-member action: RequireSpaceRole, not
	// RequirePageAccess, so an edit-link guest can't move a page out of (or
	// the shared root itself into) a location outside their link's scope.
	spaceID := db.FromPgUUID(existing.SpaceID)
	caps, ok := h.requireSpaceEditor(w, r, spaceID)
	if !ok {
		return
	}

	var req moveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	params := db.UpdatePagePositionParams{ID: db.ToPgUUID(id), Position: req.Position}
	if req.ParentID != nil {
		newParentID, ok := h.parseParentInSpace(w, r, *req.ParentID, spaceID)
		if !ok {
			return
		}
		if newParentID == id {
			httpx.WriteError(w, http.StatusBadRequest, "a page cannot be its own parent")
			return
		}
		// A page can't be moved under one of its own descendants either —
		// that would detach the subtree from the tree entirely (a cycle),
		// not just reorder it.
		descendants, err := h.queries.ListPageDescendantIDs(r.Context(), db.ToPgUUID(id))
		if err != nil {
			h.logger.Error("checking descendants before move failed", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "failed to move page")
			return
		}
		for _, d := range descendants {
			if db.FromPgUUID(d) == newParentID {
				httpx.WriteError(w, http.StatusBadRequest, "cannot move a page under its own descendant")
				return
			}
		}
		params.ParentID = db.ToPgUUID(newParentID)
	}

	page, err := h.queries.UpdatePagePosition(r.Context(), params)
	if err != nil {
		h.logger.Error("move page failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to move page")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPageResponse(page, caps))
}

// parseParentInSpace parses a client-supplied parentId and checks it names
// an existing page in spaceID. Pages can't be parented across spaces: the
// child keeps its own space_id, so a cross-space parent would put it in a
// tree its space's members can't see, under authorization it doesn't share.
func (h *Handler) parseParentInSpace(w http.ResponseWriter, r *http.Request, raw string, spaceID uuid.UUID) (uuid.UUID, bool) {
	parentID, err := uuid.Parse(raw)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid parentId")
		return uuid.UUID{}, false
	}
	parent, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(parentID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteError(w, http.StatusBadRequest, "parentId does not refer to an existing page")
			return uuid.UUID{}, false
		}
		h.logger.Error("loading parent page failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to load parent page")
		return uuid.UUID{}, false
	}
	if db.FromPgUUID(parent.SpaceID) != spaceID {
		httpx.WriteError(w, http.StatusBadRequest, "parentId must be a page in the same space")
		return uuid.UUID{}, false
	}
	return parentID, true
}
