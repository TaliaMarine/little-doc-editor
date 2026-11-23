//go:build integration

package integrationtest

import (
	"encoding/json"
	"net/http"
	"testing"
)

// my_role/can_edit (NEXT.md §1) must agree with what the server actually
// enforces for each kind of identity — the UI hides actions based on them.
func TestCallerCapabilitiesInResponses(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Capabilities Space", uniqueSlug(t))
	pageID := createPageAs(t, client, ts.URL, adminToken, spaceID)

	member := func(role string) string {
		userID, token := seedOutsider(t, pool, cfg, role)
		body, _ := json.Marshal(map[string]string{"userId": userID.String(), "role": role})
		if status, resp := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/members", adminToken, body); status != http.StatusCreated {
			t.Fatalf("adding %s: expected 201, got %d: %s", role, status, resp)
		}
		return token
	}
	_, viewLink := createShareLink(t, client, ts.URL, adminToken, pageID, map[string]string{"permission": "view"})
	_, editLink := createShareLink(t, client, ts.URL, adminToken, pageID, map[string]string{"permission": "edit"})

	cases := []struct {
		name        string
		token       string
		wantRole    *string // nil = JSON null
		wantCanEdit bool
	}{
		{"org admin reports owner", adminToken, strPtr("owner"), true},
		{"editor", member("editor"), strPtr("editor"), true},
		{"viewer", member("viewer"), strPtr("viewer"), false},
		{"view-link guest", redeemShareLink(t, client, ts.URL, viewLink, "View Guest"), nil, false},
		{"edit-link guest", redeemShareLink(t, client, ts.URL, editLink, "Edit Guest"), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := authedGet(t, client, ts.URL+"/api/v1/pages/"+pageID, tc.token)
			if status != http.StatusOK {
				t.Fatalf("GET page: expected 200, got %d: %s", status, body)
			}
			var page struct {
				MyRole  *string `json:"my_role"`
				CanEdit bool    `json:"can_edit"`
			}
			if err := json.Unmarshal(body, &page); err != nil {
				t.Fatalf("decoding page: %v", err)
			}
			if !equalStrPtr(page.MyRole, tc.wantRole) {
				t.Errorf("my_role: got %v, want %v", deref(page.MyRole), deref(tc.wantRole))
			}
			if page.CanEdit != tc.wantCanEdit {
				t.Errorf("can_edit: got %v, want %v", page.CanEdit, tc.wantCanEdit)
			}

			// can_edit must match what the server enforces.
			updateBody, _ := json.Marshal(map[string]any{"title": "Capabilities", "content": map[string]any{}})
			status, _ = authedDo(t, client, http.MethodPut, ts.URL+"/api/v1/pages/"+pageID+"/content", tc.token, updateBody)
			if allowed := status == http.StatusOK; allowed != tc.wantCanEdit {
				t.Errorf("can_edit=%v but PUT content returned %d", tc.wantCanEdit, status)
			}
		})
	}

	t.Run("non-member is refused", func(t *testing.T) {
		_, outsiderToken := seedOutsider(t, pool, cfg, "Outsider")
		if status, _ := authedGet(t, client, ts.URL+"/api/v1/pages/"+pageID, outsiderToken); status != http.StatusForbidden {
			t.Errorf("expected 403, got %d", status)
		}
	})

	t.Run("share-link scope: descendants included, everything else refused", func(t *testing.T) {
		childID := createChildPageAs(t, client, ts.URL, adminToken, spaceID, pageID)
		otherID := createPageAs(t, client, ts.URL, adminToken, spaceID)
		guest := redeemShareLink(t, client, ts.URL, editLink, "Scoped Edit Guest")

		status, body := authedGet(t, client, ts.URL+"/api/v1/pages/"+childID, guest)
		if status != http.StatusOK {
			t.Fatalf("descendant: expected 200, got %d: %s", status, body)
		}
		var child struct {
			MyRole  *string `json:"my_role"`
			CanEdit bool    `json:"can_edit"`
		}
		_ = json.Unmarshal(body, &child)
		if child.MyRole != nil || !child.CanEdit {
			t.Errorf("descendant via edit link: want my_role=null can_edit=true, got %s", body)
		}
		if status, _ := authedGet(t, client, ts.URL+"/api/v1/pages/"+otherID, guest); status != http.StatusForbidden {
			t.Errorf("out-of-scope page: expected 403, got %d", status)
		}
	})

	t.Run("write responses carry capabilities too", func(t *testing.T) {
		editorToken := member("editor")
		body, _ := json.Marshal(map[string]any{"title": "Made by editor"})
		status, resp := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/pages", editorToken, body)
		if status != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d: %s", status, resp)
		}
		var created struct {
			ID      string  `json:"id"`
			MyRole  *string `json:"my_role"`
			CanEdit bool    `json:"can_edit"`
		}
		_ = json.Unmarshal(resp, &created)
		if deref(created.MyRole) != "editor" || !created.CanEdit {
			t.Errorf("create response: want my_role=editor can_edit=true, got %s", resp)
		}

		updateBody, _ := json.Marshal(map[string]any{"title": "Renamed", "content": map[string]any{}})
		status, resp = authedDo(t, client, http.MethodPut, ts.URL+"/api/v1/pages/"+created.ID+"/content", editorToken, updateBody)
		if status != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", status, resp)
		}
		var updated struct {
			MyRole  *string `json:"my_role"`
			CanEdit bool    `json:"can_edit"`
		}
		_ = json.Unmarshal(resp, &updated)
		if deref(updated.MyRole) != "editor" || !updated.CanEdit {
			t.Errorf("update response: want my_role=editor can_edit=true, got %s", resp)
		}
	})

	t.Run("spaces report my_role", func(t *testing.T) {
		viewerToken := member("viewer")
		status, body := authedGet(t, client, ts.URL+"/api/v1/spaces/"+spaceID, viewerToken)
		if status != http.StatusOK {
			t.Fatalf("GET space: expected 200, got %d: %s", status, body)
		}
		var space struct {
			MyRole string `json:"my_role"`
		}
		_ = json.Unmarshal(body, &space)
		if space.MyRole != "viewer" {
			t.Errorf("GET space my_role: got %q, want viewer", space.MyRole)
		}

		status, body = authedGet(t, client, ts.URL+"/api/v1/spaces", viewerToken)
		if status != http.StatusOK {
			t.Fatalf("GET spaces: expected 200, got %d: %s", status, body)
		}
		var spaces []struct {
			ID     string `json:"id"`
			MyRole string `json:"my_role"`
		}
		_ = json.Unmarshal(body, &spaces)
		if len(spaces) != 1 || spaces[0].ID != spaceID || spaces[0].MyRole != "viewer" {
			t.Errorf("GET spaces: unexpected %s", body)
		}
	})
}

func strPtr(s string) *string { return &s }

func equalStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func deref(s *string) string {
	if s == nil {
		return "<null>"
	}
	return *s
}
