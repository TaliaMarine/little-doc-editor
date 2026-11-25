//go:build integration

package integrationtest

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// A share-link guest is scoped to the linked page and its descendants
// (Decision 21), not the whole space — for attachments (list, download,
// upload), export, and everything member-only (search, space export,
// history).
func TestGuestAttachmentAndExportScope(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Guest Scope Space", uniqueSlug(t))
	parentID := createPageAs(t, client, ts.URL, adminToken, spaceID)
	sharedID := createChildPageAs(t, client, ts.URL, adminToken, spaceID, parentID)
	childID := createChildPageAs(t, client, ts.URL, adminToken, spaceID, sharedID)
	siblingID := createChildPageAs(t, client, ts.URL, adminToken, spaceID, parentID)

	attach := func(pageID string) string {
		status, body := uploadAttachmentAs(t, client, ts.URL, adminToken, pageID, "a.png", "image/png", pngBytes)
		if status != http.StatusCreated {
			t.Fatalf("seed upload: %d %s", status, body)
		}
		var a struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(body, &a)
		return a.ID
	}
	inScope := map[string]string{"shared": attach(sharedID), "child": attach(childID)}
	outScope := map[string]string{"parent": attach(parentID), "sibling": attach(siblingID)}
	pages := map[string]string{"parent": parentID, "sibling": siblingID, "shared": sharedID, "child": childID}

	_, editLink := createShareLink(t, client, ts.URL, adminToken, sharedID, map[string]string{"permission": "edit"})
	guest := redeemShareLink(t, client, ts.URL, editLink, "Edit Guest")
	base := ts.URL + "/api/v1"

	for name, att := range inScope {
		if status, _ := authedGet(t, client, base+"/attachments/"+att, guest); status != http.StatusOK {
			t.Errorf("guest download of in-scope %s attachment: got %d", name, status)
		}
		if status, _ := authedGet(t, client, base+"/pages/"+pages[name]+"/attachments", guest); status != http.StatusOK {
			t.Errorf("guest list of in-scope %s: got %d", name, status)
		}
		if status, body := uploadAttachmentAs(t, client, ts.URL, guest, pages[name], "g.png", "image/png", pngBytes); status != http.StatusCreated {
			t.Errorf("edit guest upload to in-scope %s: got %d %s", name, status, body)
		}
	}
	for name, att := range outScope {
		if status, _ := authedGet(t, client, base+"/attachments/"+att, guest); status != http.StatusForbidden {
			t.Errorf("guest download of %s attachment must be 403, got %d", name, status)
		}
		if status, _ := authedGet(t, client, base+"/pages/"+pages[name]+"/attachments", guest); status != http.StatusForbidden {
			t.Errorf("guest list of %s must be 403, got %d", name, status)
		}
		if status, _ := uploadAttachmentAs(t, client, ts.URL, guest, pages[name], "g.png", "image/png", pngBytes); status != http.StatusForbidden {
			t.Errorf("edit guest upload to %s must be 403, got %d", name, status)
		}
		if status, _ := authedGet(t, client, base+"/pages/"+pages[name]+"/export?format=md", guest); status != http.StatusForbidden {
			t.Errorf("guest export of %s must be 403, got %d", name, status)
		}
	}
	for name := range inScope {
		if status, _ := authedGet(t, client, base+"/pages/"+pages[name]+"/export?format=html&descendants=true", guest); status != http.StatusOK {
			t.Errorf("guest export of in-scope %s: got %d", name, status)
		}
	}

	// Subtree export from the shared page must not leak the sibling or parent.
	e := &env{t: t, ts: ts, pool: pool, cfg: cfg}
	e.save(adminToken, siblingID, "SIBLING-SECRET", docWith("x"))
	e.save(adminToken, parentID, "PARENT-SECRET", docWith("x"))
	status, body := authedGet(t, client, base+"/pages/"+sharedID+"/export?format=html&descendants=true", guest)
	if status != http.StatusOK {
		t.Fatalf("subtree export: %d", status)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("export is not a zip: %v", err)
	}
	var names, contents []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		contents = append(contents, string(b))
	}
	all := strings.Join(append(names, contents...), "\n")
	if len(zr.File) != 2 || strings.Contains(all, "SECRET") {
		t.Errorf("subtree export should hold exactly the shared page and its child, got %v", names)
	}

	memberOnly := []string{
		"/spaces/" + spaceID,
		"/spaces/" + spaceID + "/pages",
		"/spaces/" + spaceID + "/search?q=a",
		"/spaces/" + spaceID + "/export?format=md",
		"/pages/" + sharedID + "/revisions",
		"/pages/" + sharedID + "/share-links",
		"/admin/settings",
	}
	for _, p := range memberOnly {
		if status, _ := authedGet(t, client, base+p, guest); status != http.StatusForbidden {
			t.Errorf("guest GET %s must be 403, got %d", p, status)
		}
	}
}

func TestCORSIsOptInAndPreflightSkipsAuth(t *testing.T) {
	do := func(ts string, client *http.Client, method, origin string) *http.Response {
		req, _ := http.NewRequest(method, ts+"/api/v1/spaces", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "PATCH")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	ts, _, _ := newRealAuthTestServer(t)
	if r := do(ts.URL, ts.Client(), "OPTIONS", "https://docs.example.com"); r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("CORS must be off by default, got %v", r.Header)
	}

	ts, _, _ = newRealAuthTestServerWith(t, map[string]string{"CORS_ALLOWED_ORIGINS": "https://docs.example.com"})
	r := do(ts.URL, ts.Client(), "OPTIONS", "https://docs.example.com")
	if r.StatusCode != http.StatusNoContent || r.Header.Get("Access-Control-Allow-Origin") != "https://docs.example.com" {
		t.Errorf("preflight without a token: %d %v", r.StatusCode, r.Header)
	}
	// A real request from the allowed origin still needs auth, and its 401
	// carries the CORS header so the SPA can read it.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/spaces", nil)
	req.Header.Set("Origin", "https://docs.example.com")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Access-Control-Allow-Origin") != "https://docs.example.com" {
		t.Errorf("unauthenticated cross-origin request: %d %v", resp.StatusCode, resp.Header)
	}
	if r := do(ts.URL, ts.Client(), "OPTIONS", "https://evil.example.com"); r.StatusCode != http.StatusForbidden {
		t.Errorf("preflight from unlisted origin: %d", r.StatusCode)
	}
}
