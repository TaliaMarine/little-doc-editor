package pagesapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/revisions"
	"github.com/TaliaMarine/little-doc-editor/api/internal/storage"
	"github.com/TaliaMarine/little-doc-editor/api/internal/templates"
)

func authorOf(id authctx.Identity) revisions.Author {
	var a revisions.Author
	if id.UserID != nil {
		a.UserID = db.ToPgUUID(*id.UserID)
	}
	if id.GuestIdentityID != nil {
		a.GuestID = db.ToPgUUID(*id.GuestIdentityID)
	}
	return a
}

// delete removes a page and, by FK cascade, its descendants, attachments rows,
// revisions and share links (Decision 24: editors delete content). Space
// members with editor+ only — never share-link guests, even with an edit
// link (structure changes are a member action, like move). A locked page
// must be unlocked first. Attachment blobs are removed after the rows.
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}
	page, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(id))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "page not found")
		return
	}
	if _, ok := h.requireSpaceEditor(w, r, db.FromPgUUID(page.SpaceID)); !ok {
		return
	}
	locked, err := h.queries.CountLockedPagesInSubtree(r.Context(), page.ID)
	if err != nil {
		h.logger.Error("delete page: checking locks failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to delete page")
		return
	}
	if locked > 0 {
		httpx.WriteError(w, http.StatusForbidden, "page or one of its subpages is locked")
		return
	}
	keys, err := h.queries.ListAttachmentKeysForPageTree(r.Context(), page.ID)
	if err != nil {
		h.logger.Error("delete page: listing attachments failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to delete page")
		return
	}
	if err := h.queries.DeletePage(r.Context(), page.ID); err != nil {
		h.logger.Error("delete page failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to delete page")
		return
	}
	storage.DeleteDetached(h.store, keys, h.logger)
	w.WriteHeader(http.StatusNoContent)
}

type lockRequest struct {
	Locked *bool `json:"locked"`
}

// setLock toggles the page's read-only flag. Editors and above (a lock is a
// content-protection setting, not member/space management — Decision 24);
// share-link guests can't.
func (h *Handler) setLock(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}
	page, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(id))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "page not found")
		return
	}
	caps, ok := h.requireSpaceEditor(w, r, db.FromPgUUID(page.SpaceID))
	if !ok {
		return
	}
	var req lockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Locked == nil {
		httpx.WriteError(w, http.StatusBadRequest, "locked (boolean) is required")
		return
	}
	updated, err := h.queries.SetPageLocked(r.Context(), db.SetPageLockedParams{ID: page.ID, Locked: *req.Locked})
	if err != nil {
		h.logger.Error("set page lock failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to update page")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPageResponse(updated, caps))
}

// listTemplates returns the built-in page templates (Decision 22).
func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, templates.All())
}
