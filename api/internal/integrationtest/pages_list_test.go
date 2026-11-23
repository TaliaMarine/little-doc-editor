//go:build integration

package integrationtest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestListPagesBySpace covers the sidebar tree-nav endpoint (SPEC.md §5):
// it returns every page in a space flattened (not content) in a stable,
// meaningful order, and enforces the same viewer+ space-role boundary as
// reading an individual page.
func TestListPagesBySpace(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "List Pages Space", uniqueSlug(t))
	firstPageID := createPageAs(t, client, ts.URL, adminToken, spaceID)
	secondPageID := createPageAs(t, client, ts.URL, adminToken, spaceID)
	childPageID := createChildPageAs(t, client, ts.URL, adminToken, spaceID, firstPageID)

	t.Run("space member sees every page, without content, ordered by position", func(t *testing.T) {
		status, body := authedGet(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/pages", adminToken)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", status, body)
		}

		var items []map[string]json.RawMessage
		if err := json.Unmarshal(body, &items); err != nil {
			t.Fatalf("decoding list response: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 pages, got %d: %s", len(items), body)
		}
		for _, item := range items {
			if _, ok := item["content"]; ok {
				t.Errorf("expected the tree-nav list to omit content, got it in %s", body)
			}
			if _, ok := item["title"]; !ok {
				t.Errorf("expected a title field, got %s", body)
			}
		}

		// position is scoped per (space, parent) — like the frontend's
		// childrenOf, which filters by parent_id before sorting — so the
		// only ordering guarantee is *within* a parent group, not a single
		// flat order across tree levels (a root page and a child of a
		// different parent can legitimately share position 0).
		var rootIDsInOrder []string
		parentByID := map[string]string{}
		for _, item := range items {
			var id string
			if err := json.Unmarshal(item["id"], &id); err != nil {
				t.Fatalf("decoding id: %v", err)
			}
			var parentID *string
			if err := json.Unmarshal(item["parent_id"], &parentID); err != nil {
				t.Fatalf("decoding parent_id: %v", err)
			}
			if parentID == nil {
				rootIDsInOrder = append(rootIDsInOrder, id)
			} else {
				parentByID[id] = *parentID
			}
		}

		// Before position assignment on create was fixed, every page
		// defaulted to position 0 and this order was whatever heap order
		// Postgres happened to return — not the creation order asserted here.
		wantRootIDs := []string{firstPageID, secondPageID}
		for i, want := range wantRootIDs {
			if i >= len(rootIDsInOrder) || rootIDsInOrder[i] != want {
				t.Errorf("expected root page %d to be %s, got order %v", i, want, rootIDsInOrder)
				break
			}
		}
		if got := parentByID[childPageID]; got != firstPageID {
			t.Errorf("expected the child page's parent_id to be %s, got %s", firstPageID, got)
		}
	})

	t.Run("authenticated non-member is rejected", func(t *testing.T) {
		_, outsiderToken := seedOutsider(t, pool, cfg, "Outsider")
		status, _ := authedGet(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/pages", outsiderToken)
		if status != http.StatusForbidden {
			t.Errorf("expected 403, got %d", status)
		}
	})

	t.Run("a viewer-role member is allowed", func(t *testing.T) {
		viewerID, viewerToken := seedOutsider(t, pool, cfg, "Viewer")
		addBody, _ := json.Marshal(map[string]string{"userId": viewerID.String(), "role": "viewer"})
		status, respBody := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/members", adminToken, addBody)
		if status != http.StatusCreated {
			t.Fatalf("add member: expected 201, got %d: %s", status, respBody)
		}

		status, body := authedGet(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/pages", viewerToken)
		if status != http.StatusOK {
			t.Errorf("expected 200 for a viewer-role member, got %d: %s", status, body)
		}
	})

	t.Run("a nonexistent space 404s even for an org admin", func(t *testing.T) {
		status, body := authedGet(t, client, ts.URL+"/api/v1/spaces/"+uuid.NewString()+"/pages", adminToken)
		if status != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", status, body)
		}
	})
}

func createChildPageAs(t *testing.T, client *http.Client, baseURL, token, spaceID, parentID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"title":    "Integration Test Child Page",
		"parentId": parentID,
		"content":  map[string]any{"type": "doc", "content": []any{}},
	})
	status, respBody := authedPost(t, client, baseURL+"/api/v1/spaces/"+spaceID+"/pages", token, body)
	if status != http.StatusCreated {
		t.Fatalf("create child page: expected 201, got %d: %s", status, respBody)
	}
	var page struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &page); err != nil {
		t.Fatalf("decoding child page response: %v", err)
	}
	return page.ID
}
