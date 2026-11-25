package spacesapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authz"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/storage"
)

const (
	maxSpaceNameRunes = 120
	maxIconLength     = 64
	maxHistoryCount   = 100000
	maxHistoryAgeDays = 36500
)

type updateSpaceRequest struct {
	Name              *string                `json:"name"`
	Icon              httpx.Optional[string] `json:"icon"`
	HistoryMaxCount   httpx.Optional[int32]  `json:"historyMaxCount"`
	HistoryMaxAgeDays httpx.Optional[int32]  `json:"historyMaxAgeDays"`
}

// update changes name/icon and the history policy the cleanup job reads
// (Decision 9). Space owner/admin only (Decision 24). null clears icon / a
// history limit (no limit on that dimension); an absent field is unchanged.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := h.requireAdminParam(w, r)
	if !ok {
		return
	}
	var req updateSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	params := db.UpdateSpaceParams{ID: db.ToPgUUID(spaceID)}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || utf8.RuneCountInString(name) > maxSpaceNameRunes {
			httpx.WriteError(w, http.StatusBadRequest, "name must be 1 to 120 characters")
			return
		}
		params.SetName, params.Name = true, name
	}
	if req.Icon.Set {
		if req.Icon.Value != nil && (len(*req.Icon.Value) > maxIconLength) {
			httpx.WriteError(w, http.StatusBadRequest, "icon is too long")
			return
		}
		params.SetIcon = true
		if req.Icon.Value != nil {
			params.Icon = httpx.PtrOrNil(*req.Icon.Value)
		}
	}
	if req.HistoryMaxCount.Set {
		if v := req.HistoryMaxCount.Value; v != nil && (*v < 1 || *v > maxHistoryCount) {
			httpx.WriteError(w, http.StatusBadRequest, "historyMaxCount must be between 1 and 100000, or null for no limit")
			return
		}
		params.SetMaxCount, params.HistoryMaxCount = true, req.HistoryMaxCount.Value
	}
	if req.HistoryMaxAgeDays.Set {
		if v := req.HistoryMaxAgeDays.Value; v != nil && (*v < 1 || *v > maxHistoryAgeDays) {
			httpx.WriteError(w, http.StatusBadRequest, "historyMaxAgeDays must be between 1 and 36500, or null for no limit")
			return
		}
		params.SetMaxAge, params.HistoryMaxAgeDays = true, req.HistoryMaxAgeDays.Value
	}

	space, err := h.queries.UpdateSpace(r.Context(), params)
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "space not found")
		return
	}
	myRole, err := authz.EffectiveSpaceRole(r.Context(), h.queries, authctx.FromContext(r.Context()), spaceID)
	if err != nil || myRole == nil {
		h.logger.Error("resolving space role failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to check permissions")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toSpaceResponse(space, *myRole))
}

// delete removes a space with every page, revision, attachment row and share
// link under it (FK cascade), then its attachment blobs. Owner only (an
// admin manages the space; destroying it is the owner's call). Org admins
// pass as owner.
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, authctx.FromContext(r.Context()), spaceID, authz.RoleOwner) {
		return
	}
	sid := db.ToPgUUID(spaceID)
	if _, err := h.queries.GetSpaceByID(r.Context(), sid); err != nil {
		httpx.WriteLookupError(w, h.logger, err, "space not found")
		return
	}
	keys, err := h.queries.ListAttachmentKeysForSpace(r.Context(), sid)
	if err != nil {
		h.logger.Error("delete space: listing attachments failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to delete space")
		return
	}
	if err := h.queries.DeleteSpace(r.Context(), sid); err != nil {
		h.logger.Error("delete space failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to delete space")
		return
	}
	storage.DeleteDetached(h.store, keys, h.logger)
	w.WriteHeader(http.StatusNoContent)
}
