package pagesapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
)

type revisionResponse struct {
	ID                     pgtype.UUID        `json:"id"`
	PageID                 pgtype.UUID        `json:"page_id"`
	Title                  *string            `json:"title"`
	AuthorUserID           pgtype.UUID        `json:"author_user_id"`
	AuthorGuestID          pgtype.UUID        `json:"author_guest_id"`
	AuthorUserDisplayName  *string            `json:"author_user_display_name"`
	AuthorGuestDisplayName *string            `json:"author_guest_display_name"`
	CreatedAt              pgtype.Timestamptz `json:"created_at"`
}

type revisionDetailResponse struct {
	revisionResponse
	Content json.RawMessage `json:"content"`
}

// requireHistoryViewer authorizes browsing history: any space member who can
// view the page (Decision 30: viewers browse, editors restore). Share-link
// guests are excluded — history reveals who edited and how, which a link to
// the page's current content doesn't grant.
func (h *Handler) requireHistoryViewer(w http.ResponseWriter, r *http.Request) (db.Page, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return db.Page{}, false
	}
	page, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(id))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "page not found")
		return db.Page{}, false
	}
	if authctx.FromContext(r.Context()).UserID == nil {
		httpx.WriteError(w, http.StatusForbidden, "page history is only available to space members")
		return db.Page{}, false
	}
	caps, ok := h.resolvePage(w, r, page)
	if !ok {
		return db.Page{}, false
	}
	if !caps.CanView {
		httpx.WriteError(w, http.StatusForbidden, "insufficient access to this page")
		return db.Page{}, false
	}
	return page, true
}

func (h *Handler) listRevisions(w http.ResponseWriter, r *http.Request) {
	page, ok := h.requireHistoryViewer(w, r)
	if !ok {
		return
	}
	limit, offset := int32(50), int32(0)
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 200 {
		limit = int32(n)
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n > 0 {
		offset = int32(n)
	}
	rows, err := h.queries.ListPageRevisions(r.Context(), db.ListPageRevisionsParams{PageID: page.ID, Limit: limit, Offset: offset})
	if err != nil {
		h.logger.Error("list revisions failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list revisions")
		return
	}
	out := make([]revisionResponse, len(rows))
	for i, rv := range rows {
		out[i] = revisionResponse{ID: rv.ID, PageID: rv.PageID, Title: rv.Title,
			AuthorUserID: rv.AuthorUserID, AuthorGuestID: rv.AuthorGuestID,
			AuthorUserDisplayName: rv.AuthorUserDisplayName, AuthorGuestDisplayName: rv.AuthorGuestDisplayName,
			CreatedAt: rv.CreatedAt}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) getRevision(w http.ResponseWriter, r *http.Request) {
	page, ok := h.requireHistoryViewer(w, r)
	if !ok {
		return
	}
	revID, err := uuid.Parse(chi.URLParam(r, "revisionID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid revision id")
		return
	}
	rv, err := h.queries.GetPageRevision(r.Context(), db.GetPageRevisionParams{ID: db.ToPgUUID(revID), PageID: page.ID})
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "revision not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, revisionDetailResponse{
		revisionResponse: revisionResponse{ID: rv.ID, PageID: rv.PageID, Title: rv.Title,
			AuthorUserID: rv.AuthorUserID, AuthorGuestID: rv.AuthorGuestID,
			AuthorUserDisplayName: rv.AuthorUserDisplayName, AuthorGuestDisplayName: rv.AuthorGuestDisplayName,
			CreatedAt: rv.CreatedAt},
		Content: json.RawMessage(rv.Content),
	})
}

// restoreRevision makes a past revision the page's current content (editor+,
// Decision 30; members only, page must be unlocked). The state being replaced
// is first preserved as a revision if it isn't already the latest, and the
// restore itself is recorded, so a restore is always undoable. The search
// index is refreshed like any other content mutation.
func (h *Handler) restoreRevision(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}
	revID, err := uuid.Parse(chi.URLParam(r, "revisionID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid revision id")
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
	if page.Locked {
		httpx.WriteError(w, http.StatusForbidden, "page is locked")
		return
	}
	rv, err := h.queries.GetPageRevision(r.Context(), db.GetPageRevisionParams{ID: db.ToPgUUID(revID), PageID: page.ID})
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "revision not found")
		return
	}
	author := authorOf(authctx.FromContext(r.Context()))

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.logger.Error("restore: begin tx failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to restore revision")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	q := h.queries.WithTx(tx)

	// Lock the page row and work from its current state, not the earlier
	// unlocked read: a concurrent save must not be overwritten unrecorded.
	page, err = q.GetPageByIDForUpdate(r.Context(), page.ID)
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "page not found")
		return
	}
	if page.Locked {
		httpx.WriteError(w, http.StatusForbidden, "page is locked")
		return
	}

	// Preserve the state being overwritten unless it is already the latest.
	if latest, lerr := q.GetLatestPageRevision(r.Context(), page.ID); lerr != nil || string(latest.Content) != string(page.Content) || latest.Title == nil || *latest.Title != page.Title {
		if err := h.recorder.Force(r.Context(), q, page, author); err != nil {
			h.logger.Error("restore: preserving current state failed", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "failed to restore revision")
			return
		}
	}
	title := page.Title
	if rv.Title != nil && *rv.Title != "" {
		title = *rv.Title
	}
	updated, err := q.UpdatePageContent(r.Context(), db.UpdatePageContentParams{ID: page.ID, Content: rv.Content, Title: title})
	if err != nil {
		h.logger.Error("restore: updating page failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to restore revision")
		return
	}
	if err := h.recorder.Force(r.Context(), q, updated, author); err != nil {
		h.logger.Error("restore: recording revision failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to restore revision")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.logger.Error("restore: commit failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to restore revision")
		return
	}
	if err := h.indexer.IndexPage(r.Context(), id, updated.Content); err != nil {
		h.logger.Error("indexing restored page failed", "error", err)
	}
	h.notifyContentChanged(r.Context(), id)
	httpx.WriteJSON(w, http.StatusOK, toPageResponse(updated, caps))
}
