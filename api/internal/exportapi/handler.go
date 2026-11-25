// Package exportapi implements page and space export (SPEC.md Decision 13,
// §6.9) to Markdown, HTML and print-ready HTML for PDF.
//
// PDF decision (recorded in SPEC.md): the api does not render PDFs. format=pdf
// returns one self-contained, print-optimised HTML document (page breaks
// between pages, @page margins, a CSP meta tag, images embedded as data
// URIs) that the frontend loads into a hidden iframe/window and prints
// ("Save as PDF"). That keeps the api free of a headless browser or a PDF
// library, and the output matches what the user sees in their own browser.
package exportapi

import (
	"archive/zip"
	"encoding/base64"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/authctx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/authz"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/export"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
	"github.com/TaliaMarine/little-doc-editor/api/internal/storage"
)

const (
	maxExportPages      = 2000
	maxEmbedImageBytes  = 5 << 20
	maxEmbedTotalBytes  = 25 << 20
	maxFileNameRunes    = 80
	untitled            = "Untitled"
	htmlContentType     = "text/html; charset=utf-8"
	markdownContentType = "text/markdown; charset=utf-8"
)

type Handler struct {
	queries *db.Queries
	store   storage.Storage
	logger  *slog.Logger
}

func NewHandler(queries *db.Queries, store storage.Storage, logger *slog.Logger) *Handler {
	return &Handler{queries: queries, store: store, logger: logger}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/pages/{pageID}/export", h.exportPage)
	r.Get("/spaces/{spaceID}/export", h.exportSpace)
}

type exportPage struct {
	ID       pgtype.UUID
	ParentID pgtype.UUID
	Title    string
	Content  []byte
}

func parseFormat(r *http.Request) (string, bool) {
	switch f := r.URL.Query().Get("format"); f {
	case "md", "html", "pdf":
		return f, true
	}
	return "", false
}

func (h *Handler) exportPage(w http.ResponseWriter, r *http.Request) {
	pageID, err := uuid.Parse(chi.URLParam(r, "pageID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid page id")
		return
	}
	format, ok := parseFormat(r)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "format must be one of md, html, pdf")
		return
	}
	root, err := h.queries.GetPageByID(r.Context(), db.ToPgUUID(pageID))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "page not found")
		return
	}
	// Viewing the page suffices (members at viewer+, or a guest whose link
	// covers it). With descendants, a member already sees the whole space, and
	// a guest's scope already includes the whole subtree (Decision 21).
	identity := authctx.FromContext(r.Context())
	if !authz.RequirePageAccess(r.Context(), w, h.queries, identity, db.FromPgUUID(root.SpaceID), pageID, authz.RoleViewer) {
		return
	}

	var pages []exportPage
	if r.URL.Query().Get("descendants") == "true" {
		rows, err := h.queries.ListPageSubtree(r.Context(), root.ID)
		if err != nil {
			h.logger.Error("export: listing subtree failed", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "export failed")
			return
		}
		all := make([]exportPage, len(rows))
		for i, p := range rows {
			all[i] = exportPage{ID: p.ID, ParentID: p.ParentID, Title: p.Title, Content: p.Content}
		}
		pages = orderTree(all, root.ID)
	} else {
		pages = []exportPage{{ID: root.ID, Title: root.Title, Content: root.Content}}
	}
	h.write(w, r, format, root.Title, pages, root.ID)
}

func (h *Handler) exportSpace(w http.ResponseWriter, r *http.Request) {
	spaceID, err := uuid.Parse(chi.URLParam(r, "spaceID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid space id")
		return
	}
	format, ok := parseFormat(r)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "format must be one of md, html, pdf")
		return
	}
	if !authz.RequireSpaceRole(r.Context(), w, h.queries, authctx.FromContext(r.Context()), spaceID, authz.RoleViewer) {
		return
	}
	space, err := h.queries.GetSpaceByID(r.Context(), db.ToPgUUID(spaceID))
	if err != nil {
		httpx.WriteLookupError(w, h.logger, err, "space not found")
		return
	}
	rows, err := h.queries.ListSpacePagesForExport(r.Context(), space.ID)
	if err != nil {
		h.logger.Error("export: listing space pages failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "export failed")
		return
	}
	all := make([]exportPage, len(rows))
	for i, p := range rows {
		all[i] = exportPage{ID: p.ID, ParentID: p.ParentID, Title: p.Title, Content: p.Content}
	}
	h.write(w, r, format, space.Name, orderTree(all, pgtype.UUID{}), pgtype.UUID{})
}

// orderTree returns pages depth-first. rows arrive ordered by sibling
// position; root (valid) is the subtree root, an invalid root means "every
// parentless page".
func orderTree(rows []exportPage, root pgtype.UUID) []exportPage {
	children := map[pgtype.UUID][]exportPage{}
	byID := map[pgtype.UUID]exportPage{}
	for _, p := range rows {
		byID[p.ID] = p
		children[p.ParentID] = append(children[p.ParentID], p)
	}
	var out []exportPage
	var walk func(p exportPage)
	walk = func(p exportPage) {
		out = append(out, p)
		for _, c := range children[p.ID] {
			walk(c)
		}
	}
	if root.Valid {
		if p, ok := byID[root]; ok {
			walk(p)
		}
		return out
	}
	for _, p := range rows {
		if !p.ParentID.Valid {
			walk(p)
		}
	}
	return out
}

func (h *Handler) write(w http.ResponseWriter, r *http.Request, format, name string, pages []exportPage, root pgtype.UUID) {
	if len(pages) > maxExportPages {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "too many pages to export at once")
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")

	switch format {
	case "md":
		if len(pages) == 1 {
			h.file(w, name+".md", markdownContentType, []byte(export.Markdown(export.Parse(pages[0].Content))))
			return
		}
		h.zipTree(w, name, pages, root, "md", func(p exportPage) []byte {
			return []byte(export.Markdown(export.Parse(p.Content)))
		})
	case "html":
		res := h.imageResolver(r, pages)
		render := func(p exportPage) []byte {
			body := export.HTMLBody(export.Parse(p.Content), res)
			return []byte(export.HTMLDocument(p.Title, []export.PageBody{{Title: p.Title, HTML: body}}, false))
		}
		if len(pages) == 1 {
			h.file(w, name+".html", htmlContentType, render(pages[0]))
			return
		}
		h.zipTree(w, name, pages, root, "html", render)
	case "pdf":
		res := h.imageResolver(r, pages)
		bodies := make([]export.PageBody, len(pages))
		for i, p := range pages {
			bodies[i] = export.PageBody{Title: p.Title, HTML: export.HTMLBody(export.Parse(p.Content), res)}
		}
		// Inline, not a download: the frontend loads this and calls print().
		w.Header().Set("Content-Type", htmlContentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": safeName(name) + ".html"}))
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src data: http: https:; style-src 'unsafe-inline'; sandbox allow-modals")
		_, _ = io.WriteString(w, export.HTMLDocument(name, bodies, true))
	}
}

func (h *Handler) file(w http.ResponseWriter, filename, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": sanitizeFilename(filename)}))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(body)
}

// zipTree writes pages into a zip whose directory layout mirrors the page
// tree: a page with children becomes "Title.ext" plus a "Title/" directory.
func (h *Handler) zipTree(w http.ResponseWriter, name string, pages []exportPage, root pgtype.UUID, ext string, render func(exportPage) []byte) {
	paths := treePaths(pages, root)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": sanitizeFilename(name + ".zip")}))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// Streamed to the client: a large tree is never held in memory as one zip.
	// (Headers are already sent once streaming starts, so a mid-stream failure
	// can only truncate the archive; it is logged.)
	zw := zip.NewWriter(w)
	for i, p := range pages {
		f, err := zw.Create(paths[i] + "." + ext)
		if err == nil {
			_, err = f.Write(render(p))
		}
		if err != nil {
			h.logger.Error("export: streaming zip failed", "error", err)
			return
		}
	}
	if err := zw.Close(); err != nil {
		h.logger.Error("export: closing zip failed", "error", err)
	}
}

var unsafeFileChars = regexp.MustCompile(`[\x00-\x1f\x7f/\\:*?"<>|]+`)

// safeName makes one path component from a title: no separators, control or
// reserved characters, no leading/trailing dots or spaces (so never ".."),
// bounded length.
func safeName(s string) string {
	s = unsafeFileChars.ReplaceAllString(s, "-")
	s = strings.Trim(strings.TrimSpace(s), ". ")
	if utf8.RuneCountInString(s) > maxFileNameRunes {
		s = string([]rune(s)[:maxFileNameRunes])
		s = strings.Trim(s, ". ")
	}
	if s == "" {
		return untitled
	}
	return s
}

func sanitizeFilename(s string) string {
	// keep the extension intact while sanitizing the stem
	if i := strings.LastIndex(s, "."); i > 0 {
		return safeName(s[:i]) + s[i:]
	}
	return safeName(s)
}

// treePaths assigns each page (in DFS order) a unique zip path without
// extension.
func treePaths(pages []exportPage, root pgtype.UUID) []string {
	paths := make([]string, len(pages))
	dirOf := map[pgtype.UUID]string{} // directory holding a page's children
	used := map[string]bool{}
	for i, p := range pages {
		dir := ""
		if p.ParentID.Valid && !(root.Valid && p.ID == root) {
			dir = dirOf[p.ParentID]
		}
		base := safeName(p.Title)
		cand := base
		for n := 2; used[strings.ToLower(dir+cand)]; n++ {
			cand = base + " (" + strconv.Itoa(n) + ")"
		}
		used[strings.ToLower(dir+cand)] = true
		paths[i] = dir + cand
		dirOf[p.ID] = dir + cand + "/"
	}
	return paths
}

// imageResolver embeds image attachments referenced by the exported pages as
// data: URIs. Only attachments belonging to the exported pages are eligible
// (so a crafted content reference can't pull in another page's file), only
// image/* types, and with per-image and total size caps.
func (h *Handler) imageResolver(r *http.Request, pages []exportPage) export.ImageResolver {
	inScope := map[pgtype.UUID]bool{}
	for _, p := range pages {
		inScope[p.ID] = true
	}
	total := 0 // bytes of embedded data URIs across the whole export, repeats included
	cache := map[string]string{}
	return func(id string) string {
		if uri, ok := cache[id]; ok {
			if uri == "" || total+len(uri) > maxEmbedTotalBytes {
				return ""
			}
			total += len(uri)
			return uri
		}
		cache[id] = ""
		aid, err := uuid.Parse(id)
		if err != nil {
			return ""
		}
		att, err := h.queries.GetAttachmentByID(r.Context(), db.ToPgUUID(aid))
		if err != nil || !inScope[att.PageID] || !strings.HasPrefix(att.ContentType, "image/") ||
			att.SizeBytes > maxEmbedImageBytes {
			return ""
		}
		body, err := h.store.Get(r.Context(), att.S3Key)
		if err != nil {
			h.logger.Warn("export: fetching image failed", "error", err)
			return ""
		}
		defer body.Close()
		data, err := io.ReadAll(io.LimitReader(body, maxEmbedImageBytes+1))
		if err != nil || len(data) > maxEmbedImageBytes {
			return ""
		}
		uri := "data:" + att.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data)
		if total+len(uri) > maxEmbedTotalBytes {
			return ""
		}
		total += len(uri)
		cache[id] = uri
		return uri
	}
}
