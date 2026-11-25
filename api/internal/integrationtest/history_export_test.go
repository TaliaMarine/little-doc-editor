//go:build integration

package integrationtest

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/TaliaMarine/little-doc-editor/api/internal/historycleanup"
)

type revRow struct {
	ID                    string  `json:"id"`
	Title                 *string `json:"title"`
	AuthorUserDisplayName *string `json:"author_user_display_name"`
}

func TestRevisionsRecordBrowseAndRestore(t *testing.T) {
	e := newEnv(t)
	spaceID := e.newSpace()
	editorID, editorT := e.newUser("Rev Editor")
	viewerID, viewerT := e.newUser("Rev Viewer")
	e.addMember(spaceID, editorID, "editor")
	e.addMember(spaceID, viewerID, "viewer")
	_, outsiderT := e.newUser("Rev Outsider")

	pageID := e.newPage(editorT, spaceID, "Draft", docWith("version one"), "")
	list := func(token string) []revRow {
		return decode[[]revRow](t, e.expect(http.StatusOK, "GET", "/pages/"+pageID+"/revisions", token, nil))
	}

	if revs := list(viewerT); len(revs) != 1 || revs[0].AuthorUserDisplayName == nil || *revs[0].AuthorUserDisplayName != "Rev Editor" {
		t.Fatalf("creation should record the initial revision: %+v", revs)
	}

	t.Run("debounce: rapid saves by the same author collapse", func(t *testing.T) {
		for _, w := range []string{"two", "three", "four"} {
			if status, body := e.save(editorT, pageID, "Draft", docWith("version "+w)); status != http.StatusOK {
				t.Fatalf("save: %d %s", status, body)
			}
		}
		if n := len(list(editorT)); n != 1 {
			t.Fatalf("saves within the debounce window must not add revisions, have %d", n)
		}
	})

	t.Run("a different author is not debounced", func(t *testing.T) {
		e.expect(http.StatusOK, "PUT", "/pages/"+pageID+"/content", e.adminT, map[string]any{"title": "Draft", "content": docWith("admin edit")})
		// The editor's debounced saves are not individually recorded, but their
		// last state is preserved under THEIR name before the admin's revision.
		revs := list(editorT)
		if len(revs) != 3 {
			t.Fatalf("expected 3 revisions (initial, editor's last state, admin edit), got %d", len(revs))
		}
		if revs[0].AuthorUserDisplayName == nil || *revs[0].AuthorUserDisplayName != "Feature Admin" ||
			revs[1].AuthorUserDisplayName == nil || *revs[1].AuthorUserDisplayName != "Rev Editor" {
			names := []string{}
			for _, r := range revs {
				n := "<nil>"
				if r.AuthorUserDisplayName != nil {
					n = *r.AuthorUserDisplayName
				}
				names = append(names, n)
			}
			t.Fatalf("authorship wrong (newest first): %v", names)
		}
	})

	t.Run("viewers browse, outsiders and guests cannot", func(t *testing.T) {
		revs := list(viewerT)
		e.expect(http.StatusOK, "GET", "/pages/"+pageID+"/revisions/"+revs[0].ID, viewerT, nil)
		e.expect(http.StatusForbidden, "GET", "/pages/"+pageID+"/revisions", outsiderT, nil)
		_, linkTok := createShareLink(t, e.ts.Client(), e.ts.URL, editorT, pageID, map[string]string{"permission": "edit"})
		guestT := redeemShareLink(t, e.ts.Client(), e.ts.URL, linkTok, "Guesty")
		e.expect(http.StatusForbidden, "GET", "/pages/"+pageID+"/revisions", guestT, nil)
		e.expect(http.StatusNotFound, "GET", "/pages/"+pageID+"/revisions/"+pageID, viewerT, nil)
	})

	t.Run("restore: editors only, search index refreshed, undoable", func(t *testing.T) {
		revs := list(editorT) // newest first
		oldest := revs[len(revs)-1]
		detail := decode[struct {
			Content map[string]any `json:"content"`
		}](t, e.expect(http.StatusOK, "GET", "/pages/"+pageID+"/revisions/"+oldest.ID, viewerT, nil))
		if !strings.Contains(mustJSON(detail.Content), "version one") {
			t.Fatalf("oldest revision should hold the initial content: %v", detail.Content)
		}

		e.expect(http.StatusForbidden, "POST", "/pages/"+pageID+"/revisions/"+oldest.ID+"/restore", viewerT, nil)
		restored := decode[struct {
			Content map[string]any `json:"content"`
		}](t, e.expect(http.StatusOK, "POST", "/pages/"+pageID+"/revisions/"+oldest.ID+"/restore", editorT, nil))
		if !strings.Contains(mustJSON(restored.Content), "version one") {
			t.Fatalf("page not restored: %v", restored.Content)
		}
		got := decode[struct {
			Content map[string]any `json:"content"`
		}](t, e.expect(http.StatusOK, "GET", "/pages/"+pageID, editorT, nil))
		if !strings.Contains(mustJSON(got.Content), "version one") {
			t.Fatal("restore did not persist")
		}
		// the restored words are searchable, the replaced ones are not
		if !strings.Contains(string(e.expect(http.StatusOK, "GET", "/spaces/"+spaceID+"/search?q=one", editorT, nil)), pageID) {
			t.Fatal("search index must be updated by restore")
		}
		if strings.Contains(string(e.expect(http.StatusOK, "GET", "/spaces/"+spaceID+"/search?q=admin", editorT, nil)), pageID) {
			t.Fatal("stale text still indexed after restore")
		}
		// the state that was replaced is preserved, and the restore itself recorded
		after := list(editorT)
		if len(after) <= len(revs) {
			t.Fatalf("restore must add revisions (before %d, after %d)", len(revs), len(after))
		}
		// locked pages refuse restore
		e.expect(http.StatusOK, "PATCH", "/pages/"+pageID+"/lock", editorT, map[string]bool{"locked": true})
		e.expect(http.StatusForbidden, "POST", "/pages/"+pageID+"/revisions/"+oldest.ID+"/restore", editorT, nil)
	})

	t.Run("history can be disabled per page", func(t *testing.T) {
		off := e.newPage(editorT, spaceID, "No history", docWith("a"), "")
		e.exec(`UPDATE pages SET history_enabled = false WHERE id = $1`, off)
		e.expect(http.StatusOK, "PUT", "/pages/"+off+"/content", e.adminT, map[string]any{"content": docWith("b")})
		var n int
		if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM page_revisions WHERE page_id = $1`, off).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 { // only the creation snapshot taken before history was switched off
			t.Fatalf("history disabled: expected only the initial revision, got %d", n)
		}
	})

	t.Run("pre-history pages get a baseline on first save", func(t *testing.T) {
		old := e.newPage(editorT, spaceID, "Old page", docWith("legacy text"), "")
		e.exec(`DELETE FROM page_revisions WHERE page_id = $1`, old)
		e.expect(http.StatusOK, "PUT", "/pages/"+old+"/content", editorT, map[string]any{"title": "Old page", "content": docWith("new text")})
		revs := decode[[]revRow](t, e.expect(http.StatusOK, "GET", "/pages/"+old+"/revisions", editorT, nil))
		if len(revs) != 2 {
			t.Fatalf("expected baseline + new revision, got %d", len(revs))
		}
	})
}

func mustJSON(v any) string {
	b := new(bytes.Buffer)
	_ = jsonEnc(b, v)
	return b.String()
}

func TestHistoryCleanupJob(t *testing.T) {
	e := newEnv(t)
	spaceID := e.newSpace()
	pageA := e.newPage(e.adminT, spaceID, "A", docWith("a"), "")
	pageB := e.newPage(e.adminT, spaceID, "B", docWith("b"), "")
	for _, p := range []string{pageA, pageB} {
		e.exec(`DELETE FROM page_revisions WHERE page_id = $1`, p)
		for i := 0; i < 5; i++ {
			// i days old, oldest first by created_at
			e.exec(`INSERT INTO page_revisions (page_id, content, title, created_at) VALUES ($1, '{}', 't', now() - make_interval(days => $2))`, p, 10-i*2)
		}
	}
	job := historycleanup.New(e.pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	count := func(p string) int {
		var n int
		if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM page_revisions WHERE page_id = $1`, p).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	logs := func() int {
		var n int
		if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM history_cleanup_logs WHERE space_id = $1`, spaceID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("no policy: nothing deleted, nothing logged", func(t *testing.T) {
		if err := job.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if count(pageA) != 5 || logs() != 0 {
			t.Fatal("space without a policy must be untouched")
		}
	})

	t.Run("max count", func(t *testing.T) {
		e.expect(http.StatusOK, "PATCH", "/spaces/"+spaceID, e.adminT, map[string]any{"historyMaxCount": 3})
		if err := job.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if count(pageA) != 3 || count(pageB) != 3 || logs() != 1 {
			t.Fatalf("count policy: A=%d B=%d logs=%d", count(pageA), count(pageB), logs())
		}
		var deleted int
		if err := e.pool.QueryRow(context.Background(), `SELECT deleted_count FROM history_cleanup_logs WHERE space_id = $1`, spaceID).Scan(&deleted); err != nil || deleted != 4 {
			t.Fatalf("log should report 4 deleted (2 per page), got %d err=%v", deleted, err)
		}
	})

	t.Run("a run that deletes nothing is not logged", func(t *testing.T) {
		if err := job.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if logs() != 1 {
			t.Fatalf("no-op run must not log, have %d entries", logs())
		}
	})

	t.Run("max age (whichever fires first)", func(t *testing.T) {
		// remaining revisions are 6, 4, 2 days old; keep < 5 days and 3 newest
		e.expect(http.StatusOK, "PATCH", "/spaces/"+spaceID, e.adminT, map[string]any{"historyMaxAgeDays": 5})
		if err := job.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if count(pageA) != 2 || logs() != 2 {
			t.Fatalf("age policy: A=%d logs=%d", count(pageA), logs())
		}
	})

	t.Run("concurrent runs are safe", func(t *testing.T) {
		done := make(chan error, 4)
		for i := 0; i < 4; i++ {
			go func() { done <- job.RunOnce(context.Background()) }()
		}
		for i := 0; i < 4; i++ {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("schedule spec is parsed", func(t *testing.T) {
		stop, err := job.Start("@every 1h")
		if err != nil {
			t.Fatal(err)
		}
		stop()
		if _, err := job.Start("every now and then"); err == nil {
			t.Fatal("invalid schedule must be rejected")
		}
		if stop, err := job.Start("off"); err != nil {
			t.Fatal(err)
		} else {
			stop()
		}
	})
}

func TestExport(t *testing.T) {
	e := newEnv(t)
	spaceID := e.newSpace()
	viewerID, viewerT := e.newUser("Export Viewer")
	e.addMember(spaceID, viewerID, "viewer")
	_, outsiderT := e.newUser("Export Outsider")

	rich := map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "heading", "attrs": map[string]any{"level": 2}, "content": []any{map[string]any{"type": "text", "text": "Hello <script>alert(1)</script>"}}},
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "bold", "marks": []any{map[string]any{"type": "bold"}}},
		}},
	}}
	root := e.newPage(e.adminT, spaceID, "Root/Page: One", rich, "")
	child := e.newPage(e.adminT, spaceID, "Child", docWith("child text"), root)
	_ = child
	e.newPage(e.adminT, spaceID, "Other root", docWith("other"), "")

	raw := func(path, token string, want int) (http.Header, []byte) {
		t.Helper()
		req, _ := http.NewRequest("GET", e.ts.URL+"/api/v1"+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := e.ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("GET %s: expected %d, got %d: %s", path, want, resp.StatusCode, b)
		}
		return resp.Header, b
	}

	t.Run("single page markdown", func(t *testing.T) {
		h, body := raw("/pages/"+root+"/export?format=md", viewerT, 200)
		if !strings.Contains(h.Get("Content-Disposition"), "attachment") || !strings.Contains(h.Get("Content-Type"), "markdown") {
			t.Fatalf("headers: %v", h)
		}
		if !strings.Contains(string(body), "## Hello \\<script>alert(1)\\</script>") || !strings.Contains(string(body), "**bold**") {
			t.Fatalf("unexpected markdown: %s", body)
		}
		if strings.Contains(h.Get("Content-Disposition"), "/") {
			t.Fatalf("filename must be sanitized: %s", h.Get("Content-Disposition"))
		}
	})

	t.Run("html is escaped and self-protecting", func(t *testing.T) {
		_, body := raw("/pages/"+root+"/export?format=html", viewerT, 200)
		s := string(body)
		if strings.Contains(s, "<script>alert") || !strings.Contains(s, "&lt;script&gt;") || !strings.Contains(s, "Content-Security-Policy") {
			t.Fatalf("html export not safe: %s", s)
		}
	})

	t.Run("descendants export is a zip mirroring the tree", func(t *testing.T) {
		_, body := raw("/pages/"+root+"/export?format=md&descendants=true", viewerT, 200)
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, f := range zr.File {
			names = append(names, f.Name)
			if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") || strings.Count(f.Name, ":") > 0 {
				t.Fatalf("unsafe zip path %q", f.Name)
			}
		}
		joined := strings.Join(names, "|")
		if len(names) != 2 || !strings.Contains(joined, "Root-Page- One.md") || !strings.Contains(joined, "Root-Page- One/Child.md") {
			t.Fatalf("unexpected zip layout: %v", names)
		}
	})

	t.Run("space export and pdf (print html)", func(t *testing.T) {
		_, body := raw("/spaces/"+spaceID+"/export?format=md", viewerT, 200)
		zr, _ := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if len(zr.File) != 3 {
			t.Fatalf("space export should hold all 3 pages, got %d", len(zr.File))
		}
		h, body := raw("/spaces/"+spaceID+"/export?format=pdf", viewerT, 200)
		if !strings.HasPrefix(h.Get("Content-Type"), "text/html") || !strings.Contains(string(body), "@page") || strings.Count(string(body), `class="page"`) != 3 {
			t.Fatalf("pdf format must be one print-ready html document: %s", body)
		}
	})

	t.Run("authorization and validation", func(t *testing.T) {
		raw("/pages/"+root+"/export?format=md", outsiderT, 403)
		raw("/spaces/"+spaceID+"/export?format=md", outsiderT, 403)
		raw("/pages/"+root+"/export?format=docx", viewerT, 400)
		raw("/pages/"+root+"/export", viewerT, 400)
		raw("/pages/"+child+"/export?format=md", viewerT, 200)
	})

	t.Run("view-link guests export only their scope", func(t *testing.T) {
		_, linkTok := createShareLink(t, e.ts.Client(), e.ts.URL, e.adminT, child, map[string]string{"permission": "view"})
		guestT := redeemShareLink(t, e.ts.Client(), e.ts.URL, linkTok, "G")
		raw("/pages/"+child+"/export?format=html", guestT, 200)
		raw("/pages/"+root+"/export?format=html", guestT, 403)
		raw("/spaces/"+spaceID+"/export?format=md", guestT, 403)
	})
}
