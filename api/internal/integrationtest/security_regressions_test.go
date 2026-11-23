//go:build integration

package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
)

// Regression coverage for authorization/sharing holes found in review, plus
// boundary cases (scope, permission, visibility) the older tests only
// covered from the "access granted" side.

func TestShareGuestSessionEndsWhenLinkIsRevoked(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Revocation Space", uniqueSlug(t))
	pageID := createPageAs(t, client, ts.URL, adminToken, spaceID)

	linkID, linkToken := createShareLink(t, client, ts.URL, adminToken, pageID, map[string]string{"permission": "view"})
	guestToken := redeemShareLink(t, client, ts.URL, linkToken, "Soon Revoked")

	if status, body := authedGet(t, client, ts.URL+"/api/v1/pages/"+pageID, guestToken); status != http.StatusOK {
		t.Fatalf("guest should see the page before revocation, got %d: %s", status, body)
	}

	if status, _ := authedDo(t, client, http.MethodDelete, ts.URL+"/api/v1/share-links/"+linkID, adminToken, nil); status != http.StatusNoContent {
		t.Fatalf("revoking link: expected 204, got %d", status)
	}

	// The guest's JWT is still unexpired — revocation must cut it off anyway.
	if status, _ := authedGet(t, client, ts.URL+"/api/v1/pages/"+pageID, guestToken); status != http.StatusUnauthorized {
		t.Errorf("guest session from a revoked link should be rejected with 401, got %d", status)
	}

	body, _ := json.Marshal(map[string]string{"displayName": "Too Late"})
	resp, err := client.Post(ts.URL+"/api/v1/share/"+linkToken+"/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("redeem request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Errorf("redeeming a revoked link should be 410, got %d", resp.StatusCode)
	}
}

func TestShareGuestScopeAndPermissionBoundaries(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Scope Space", uniqueSlug(t))
	parentID := createPageAs(t, client, ts.URL, adminToken, spaceID)
	sharedID := createChildPageAs(t, client, ts.URL, adminToken, spaceID, parentID)
	siblingID := createChildPageAs(t, client, ts.URL, adminToken, spaceID, parentID)

	_, viewLink := createShareLink(t, client, ts.URL, adminToken, sharedID, map[string]string{"permission": "view"})
	viewGuest := redeemShareLink(t, client, ts.URL, viewLink, "Viewer Guest")

	for name, id := range map[string]string{"parent": parentID, "sibling": siblingID} {
		if status, _ := authedGet(t, client, ts.URL+"/api/v1/pages/"+id, viewGuest); status != http.StatusForbidden {
			t.Errorf("guest scoped to a child page must not read its %s, got %d", name, status)
		}
	}

	if status, body := uploadAttachmentAs(t, client, ts.URL, viewGuest, sharedID, "x.png", "image/png", pngBytes); status != http.StatusForbidden {
		t.Errorf("view-link guest must not upload, got %d: %s", status, body)
	}

	t.Run("blank display name is rejected server-side", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"displayName": "   "})
		resp, err := client.Post(ts.URL+"/api/v1/share/"+viewLink+"/redeem", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("redeem request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestAccessLogVisibilityAndLockedUploads(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Access Log Space", uniqueSlug(t))
	pageID := createPageAs(t, client, ts.URL, adminToken, spaceID)
	linkID, _ := createShareLink(t, client, ts.URL, adminToken, pageID, map[string]string{"permission": "view"})

	addMember := func(role string) string {
		userID, token := seedOutsider(t, pool, cfg, role)
		body, _ := json.Marshal(map[string]string{"userId": userID.String(), "role": role})
		if status, resp := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/members", adminToken, body); status != http.StatusCreated {
			t.Fatalf("adding %s: expected 201, got %d: %s", role, status, resp)
		}
		return token
	}
	editorToken := addMember("editor")
	viewerToken := addMember("viewer")

	logURL := ts.URL + "/api/v1/share-links/" + linkID + "/access-log"
	if status, _ := authedGet(t, client, logURL, editorToken); status != http.StatusOK {
		t.Errorf("editor should read the access log (Decision 16), got %d", status)
	}
	if status, _ := authedGet(t, client, logURL, viewerToken); status != http.StatusForbidden {
		t.Errorf("viewer must not read the access log, got %d", status)
	}

	if _, err := pool.Exec(context.Background(), "UPDATE pages SET locked = true WHERE id = $1", pageID); err != nil {
		t.Fatalf("locking page: %v", err)
	}
	if status, body := uploadAttachmentAs(t, client, ts.URL, editorToken, pageID, "x.png", "image/png", pngBytes); status != http.StatusForbidden {
		t.Errorf("upload to a locked page should be 403, got %d: %s", status, body)
	}
}

func TestEditLinkGuestCannotManageLinksOrRestructureTree(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Guest Limits Space", uniqueSlug(t))
	sharedPageID := createPageAs(t, client, ts.URL, adminToken, spaceID)
	childID := createChildPageAs(t, client, ts.URL, adminToken, spaceID, sharedPageID)
	otherRootID := createPageAs(t, client, ts.URL, adminToken, spaceID)

	_, linkToken := createShareLink(t, client, ts.URL, adminToken, sharedPageID, map[string]string{"permission": "edit"})
	guestToken := redeemShareLink(t, client, ts.URL, linkToken, "Edit Guest")

	// Sanity: the edit link really does grant content editing in scope.
	updateBody, _ := json.Marshal(map[string]any{"title": "Edited by guest", "content": map[string]any{}})
	if status, body := authedDo(t, client, http.MethodPut, ts.URL+"/api/v1/pages/"+childID+"/content", guestToken, updateBody); status != http.StatusOK {
		t.Fatalf("edit guest should be able to edit an in-scope page, got %d: %s", status, body)
	}

	t.Run("cannot list share links (would expose other links' tokens)", func(t *testing.T) {
		if status, _ := authedGet(t, client, ts.URL+"/api/v1/pages/"+sharedPageID+"/share-links", guestToken); status != http.StatusForbidden {
			t.Errorf("expected 403, got %d", status)
		}
	})

	t.Run("cannot create share links", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"permission": "edit"})
		if status, _ := authedPost(t, client, ts.URL+"/api/v1/pages/"+sharedPageID+"/share-links", guestToken, body); status != http.StatusForbidden {
			t.Errorf("expected 403, got %d", status)
		}
	})

	t.Run("cannot move pages", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"parentId": otherRootID, "position": 1})
		if status, _ := authedDo(t, client, http.MethodPatch, ts.URL+"/api/v1/pages/"+childID+"/move", guestToken, body); status != http.StatusForbidden {
			t.Errorf("expected 403, got %d", status)
		}
	})
}

func TestShareLinkResponsesOmitPasswordHash(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Hash Leak Space", uniqueSlug(t))
	pageID := createPageAs(t, client, ts.URL, adminToken, spaceID)

	body, _ := json.Marshal(map[string]string{"permission": "view", "password": "s3cret"})
	status, created := authedPost(t, client, ts.URL+"/api/v1/pages/"+pageID+"/share-links", adminToken, body)
	if status != http.StatusCreated {
		t.Fatalf("creating link: expected 201, got %d: %s", status, created)
	}
	status, listed := authedGet(t, client, ts.URL+"/api/v1/pages/"+pageID+"/share-links", adminToken)
	if status != http.StatusOK {
		t.Fatalf("listing links: expected 200, got %d: %s", status, listed)
	}

	for name, raw := range map[string][]byte{"create": created, "list": listed} {
		if bytes.Contains(raw, []byte("password_hash")) || bytes.Contains(raw, []byte("$argon2id$")) {
			t.Errorf("%s response leaks the password hash: %s", name, raw)
		}
		if !bytes.Contains(raw, []byte(`"requires_password":true`)) {
			t.Errorf("%s response should report requires_password: %s", name, raw)
		}
	}
}

func TestPageParentMustBeInSameSpace(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceA := createSpaceAs(t, client, ts.URL, adminToken, "Space A", uniqueSlug(t))
	spaceB := createSpaceAs(t, client, ts.URL, adminToken, "Space B", uniqueSlug(t))
	pageA := createPageAs(t, client, ts.URL, adminToken, spaceA)
	pageB := createPageAs(t, client, ts.URL, adminToken, spaceB)

	t.Run("create under a parent in another space is rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"title": "Cross", "parentId": pageB})
		if status, resp := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceA+"/pages", adminToken, body); status != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", status, resp)
		}
	})

	t.Run("create under a nonexistent parent is a 400, not a 500", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"title": "Orphan", "parentId": "00000000-0000-0000-0000-0000000000aa"})
		if status, resp := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceA+"/pages", adminToken, body); status != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", status, resp)
		}
	})

	t.Run("move under a parent in another space is rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"parentId": pageB, "position": 1})
		if status, resp := authedDo(t, client, http.MethodPatch, ts.URL+"/api/v1/pages/"+pageA+"/move", adminToken, body); status != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", status, resp)
		}
	})
}

func TestSpaceErrorsAreClientErrors(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	slug := uniqueSlug(t)
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Dup Space", slug)

	t.Run("duplicate slug is 409", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "Dup Again", "slug": slug})
		if status, resp := authedPost(t, client, ts.URL+"/api/v1/spaces", adminToken, body); status != http.StatusConflict {
			t.Errorf("expected 409, got %d: %s", status, resp)
		}
	})

	t.Run("adding a nonexistent user is 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"userId": "00000000-0000-0000-0000-0000000000bb", "role": "viewer"})
		if status, resp := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/members", adminToken, body); status != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", status, resp)
		}
	})

	t.Run("updating a non-member is 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"role": "editor"})
		url := ts.URL + "/api/v1/spaces/" + spaceID + "/members/00000000-0000-0000-0000-0000000000cc"
		if status, resp := authedDo(t, client, http.MethodPatch, url, adminToken, body); status != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", status, resp)
		}
	})
}

func TestAttachmentListAndDownloadHeaders(t *testing.T) {
	ts := newTestServer(t)
	client := ts.Client()

	spaceID := createSpace(t, client, ts.URL, "Attachment List Space", uniqueSlug(t))
	pageID := createPage(t, client, ts.URL, spaceID)

	status, body := uploadAttachment(t, client, ts.URL, pageID, `we"ird; name.png`, "image/png", pngBytes)
	if status != http.StatusCreated {
		t.Fatalf("upload: expected 201, got %d: %s", status, body)
	}
	if bytes.Contains(body, []byte("s3_key")) {
		t.Errorf("upload response leaks s3_key: %s", body)
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &uploaded); err != nil {
		t.Fatalf("decoding upload: %v", err)
	}

	status, listBody := authedGet(t, client, ts.URL+"/api/v1/pages/"+pageID+"/attachments", devAdminToken(t))
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", status, listBody)
	}
	var listed []struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(listBody, &listed); err != nil {
		t.Fatalf("decoding list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != uploaded.ID || listed[0].Filename != `we"ird; name.png` {
		t.Errorf("unexpected list contents: %s", listBody)
	}
	if bytes.Contains(listBody, []byte("s3_key")) {
		t.Errorf("list response leaks s3_key: %s", listBody)
	}

	resp, err := client.Get(ts.URL + "/api/v1/attachments/" + uploaded.ID)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected X-Content-Type-Options: nosniff, got %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Errorf("expected a sandboxing CSP on downloads, got %q", got)
	}
	// The quote and semicolon in the filename must stay inside one quoted
	// filename parameter, not terminate it.
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment; ") || !strings.Contains(cd, `\"`) {
		t.Errorf("filename not safely quoted in Content-Disposition: %q", cd)
	}

	t.Run("body far over the size cap is rejected", func(t *testing.T) {
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		huge := append(append([]byte{}, pngBytes...), make([]byte, cfg.AttachmentMaxSizeBytes+2<<20)...)
		status, body := uploadAttachment(t, client, ts.URL, pageID, "huge.png", "image/png", huge)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", status, body)
		}
	})
}

func createShareLink(t *testing.T, client *http.Client, baseURL, token, pageID string, req map[string]string) (id, linkToken string) {
	t.Helper()
	body, _ := json.Marshal(req)
	status, resp := authedPost(t, client, baseURL+"/api/v1/pages/"+pageID+"/share-links", token, body)
	if status != http.StatusCreated {
		t.Fatalf("creating share link: expected 201, got %d: %s", status, resp)
	}
	var link struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp, &link); err != nil {
		t.Fatalf("decoding share link: %v", err)
	}
	return link.ID, link.Token
}

func redeemShareLink(t *testing.T, client *http.Client, baseURL, linkToken, displayName string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"displayName": displayName})
	resp, err := client.Post(baseURL+"/api/v1/share/"+linkToken+"/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("redeem request: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("redeem: expected 200, got %d: %s", resp.StatusCode, respBody)
	}
	var redeemed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &redeemed); err != nil {
		t.Fatalf("decoding redeem response: %v", err)
	}
	return redeemed.Token
}

func authedDo(t *testing.T, client *http.Client, method, url, token string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody
}
