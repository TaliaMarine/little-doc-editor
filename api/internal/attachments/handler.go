package attachments

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authz"
	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/storage"
)

// maxMultipartMemory caps how much of a multipart upload chi/net-http buffers
// in memory before spilling to a temp file — independent of, and smaller
// than, the attachment size limit itself.
const maxMultipartMemory = 10 << 20 // 10MB

// multipartOverhead is headroom on top of the attachment size limit for the
// multipart envelope (boundaries, part headers, any small extra fields), so
// the whole request body can be capped without rejecting a file that's
// exactly at the limit.
const multipartOverhead = 1 << 20 // 1MB

type Handler struct {
	queries *db.Queries
	store   storage.Storage
	cfg     config.Config
	logger  *slog.Logger
}

func NewHandler(queries *db.Queries, store storage.Storage, cfg config.Config, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, store: store, cfg: cfg, logger: logger}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/pages/{pageID}/attachments", h.upload)
	r.Get("/pages/{pageID}/attachments", h.list)
	r.Get("/attachments/{attachmentID}", h.download)
}

// attachmentResponse is db.Attachment minus s3_key — the object-storage
// layout is an internal detail (download goes through this API, never
// straight to the bucket), not part of the public contract.
type attachmentResponse struct {
	ID                pgtype.UUID        `json:"id"`
	PageID            pgtype.UUID        `json:"page_id"`
	Filename          string             `json:"filename"`
	ContentType       string             `json:"content_type"`
	SizeBytes         int64              `json:"size_bytes"`
	UploadedByUserID  pgtype.UUID        `json:"uploaded_by_user_id"`
	UploadedByGuestID pgtype.UUID        `json:"uploaded_by_guest_id"`
	CreatedAt         pgtype.Timestamptz `json:"created_at"`
}

func toAttachmentResponse(a db.Attachment) attachmentResponse {
	return attachmentResponse{
		ID:                a.ID,
		PageID:            a.PageID,
		Filename:          a.Filename,
		ContentType:       a.ContentType,
		SizeBytes:         a.SizeBytes,
		UploadedByUserID:  a.UploadedByUserID,
		UploadedByGuestID: a.UploadedByGuestID,
		CreatedAt:         a.CreatedAt,
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	pageID, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}
	page, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(pageID))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "page not found")
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequirePageAccess(r.Context(), w, h.queries, identity, db.FromPgUUID(page.SpaceID), pageID, authz.RoleViewer) {
		return
	}

	attachments, err := h.queries.ListAttachmentsForPage(r.Context(), db.ToPgUUID(pageID))
	if err != nil {
		h.logger.Error("list attachments failed", "error", err, "page_id", pageID)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list attachments")
		return
	}
	items := make([]attachmentResponse, len(attachments))
	for i, a := range attachments {
		items[i] = toAttachmentResponse(a)
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	pageID, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}

	page, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(pageID))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "page not found")
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequirePageAccess(r.Context(), w, h.queries, identity, db.FromPgUUID(page.SpaceID), pageID, authz.RoleEditor) {
		return
	}
	// Same rule as content writes (pagesapi.updateContent) and collab: a
	// locked page is read-only for everyone.
	if page.Locked {
		httpx.WriteError(w, http.StatusForbidden, "page is locked")
		return
	}

	// Cap the whole request body, not just the bytes read from the file
	// part below: ParseMultipartForm otherwise spools an arbitrarily large
	// body to a temp file on disk before any size check runs.
	// The effective policy (Decision 17): the admin-set instance settings
	// when present, else the deployment defaults from env/Helm.
	settings, err := h.queries.GetInstanceSettings(r.Context())
	if err != nil {
		h.logger.Error("upload: reading instance settings failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to load upload policy")
		return
	}
	policy := EffectivePolicy(h.cfg, settings)

	r.Body = http.MaxBytesReader(w, r.Body, policy.MaxSizeBytes+multipartOverhead)
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpx.WriteError(w, http.StatusUnprocessableEntity, ErrTooLarge.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "could not parse multipart form: "+err.Error())
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "missing \"file\" field: "+err.Error())
		return
	}
	defer file.Close()

	// Read the whole upload into memory rather than trusting header.Size
	// (an attacker-controlled multipart header, not the real byte count) —
	// LimitReader caps how much we'll ever read to maxSize+1, so a lie in
	// either direction is caught by the actual length below rather than by
	// what the client claimed. This also gives storage.Put a seekable
	// io.ReadSeeker, which the S3 SDK's request signing requires.
	data, err := io.ReadAll(io.LimitReader(file, policy.MaxSizeBytes+1))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "could not read upload: "+err.Error())
		return
	}

	// Validate must see real file content, never the client's claimed
	// Content-Type or the filename extension (SPEC.md §8).
	contentType, err := Validate(data, int64(len(data)), policy.Allowlist, policy.MaxSizeBytes)
	if err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	attachmentID := uuid.New()
	s3Key := "attachments/" + pageID.String() + "/" + attachmentID.String()

	if err := h.store.Put(r.Context(), s3Key, contentType, bytes.NewReader(data), int64(len(data))); err != nil {
		h.logger.Error("attachment upload to object storage failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "failed to store attachment")
		return
	}

	params := db.CreateAttachmentParams{
		PageID:      db.ToPgUUID(pageID),
		S3Key:       s3Key,
		Filename:    header.Filename,
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
	}
	if identity.UserID != nil {
		params.UploadedByUserID = db.ToPgUUID(*identity.UserID)
	}
	if identity.GuestIdentityID != nil {
		params.UploadedByGuestID = db.ToPgUUID(*identity.GuestIdentityID)
	}

	attachment, err := h.queries.CreateAttachment(r.Context(), params)
	if err != nil {
		h.logger.Error("attachment metadata insert failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to record attachment")
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toAttachmentResponse(attachment))
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	attachmentID, err := uuid.Parse(chi.URLParam(r, "attachmentID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid attachment id")
		return
	}

	attachment, err := h.queries.GetAttachmentByID(r.Context(), db.ToPgUUID(attachmentID))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "attachment not found")
		return
	}
	page, err := h.queries.GetPageByID(r.Context(), attachment.PageID)
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "page not found")
		return
	}
	identity := authctx.FromContext(r.Context())
	if !authz.RequirePageAccess(r.Context(), w, h.queries, identity, db.FromPgUUID(page.SpaceID), db.FromPgUUID(attachment.PageID), authz.RoleViewer) {
		return
	}

	body, err := h.store.Get(r.Context(), attachment.S3Key)
	if err != nil {
		h.logger.Error("attachment fetch from object storage failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "failed to fetch attachment")
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", attachment.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(attachment.SizeBytes, 10))
	// mime.FormatMediaType quotes/escapes the client-supplied filename (and
	// RFC 2231-encodes non-ASCII), so a name containing `"` or `;` can't
	// break out of the parameter.
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Filename}))
	// Served from the API's own origin: never let a browser re-sniff the
	// stored type, and never let it execute anything (e.g. an allowlisted
	// SVG's script) if the file is ever opened directly.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if _, err := io.Copy(w, body); err != nil {
		h.logger.Error("streaming attachment to client failed", "error", err)
	}
}
