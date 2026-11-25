//go:build integration

package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type memberJSON struct {
	UserID    *string `json:"user_id"`
	GroupID   *string `json:"group_id"`
	GroupName *string `json:"group_name"`
	UserEmail *string `json:"user_email"`
	Role      string  `json:"role"`
}

func TestSpaceMemberManagementAndLastOwnerGuard(t *testing.T) {
	e := newEnv(t)
	spaceID := e.newSpace()
	ownerID, ownerT := e.newUser("Space Owner")
	adminID, adminT := e.newUser("Space Admin")
	editorID, editorT := e.newUser("Space Editor")
	e.addMember(spaceID, ownerID, "owner")
	e.addMember(spaceID, adminID, "admin")
	e.addMember(spaceID, editorID, "editor")
	// the org admin who created the space is the space's first owner too
	base := "/spaces/" + spaceID + "/members"

	t.Run("list shows names", func(t *testing.T) {
		members := decode[[]memberJSON](t, e.expect(http.StatusOK, "GET", base, editorT, nil))
		if len(members) != 4 {
			t.Fatalf("expected 4 members, got %d", len(members))
		}
	})

	t.Run("editors cannot manage members", func(t *testing.T) {
		e.expect(http.StatusForbidden, "POST", base, editorT, map[string]string{"userId": uuid.NewString(), "role": "viewer"})
		e.expect(http.StatusForbidden, "PATCH", base+"/"+editorID.String(), editorT, map[string]string{"role": "owner"})
		e.expect(http.StatusForbidden, "DELETE", base+"/"+adminID.String(), editorT, nil)
	})

	t.Run("candidates picker for space admins", func(t *testing.T) {
		if out := e.expect(http.StatusOK, "GET", "/spaces/"+spaceID+"/member-candidates?q=__", adminT, nil); strings.TrimSpace(string(out)) != "[]" {
			t.Fatalf("LIKE wildcards in q must be literal, got %s", out)
		}
		zebra := "Zebra" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, _ = e.newUser(zebra)
		e.expect(http.StatusBadRequest, "GET", "/spaces/"+spaceID+"/member-candidates?q=z", adminT, nil)
		e.expect(http.StatusForbidden, "GET", "/spaces/"+spaceID+"/member-candidates?q=zebra", editorT, nil)
		out := e.expect(http.StatusOK, "GET", "/spaces/"+spaceID+"/member-candidates?q="+zebra, adminT, nil)
		if !strings.Contains(string(out), zebra) || strings.Contains(string(out), "password") {
			t.Fatalf("unexpected candidates: %s", out)
		}
		if strings.Contains(string(e.expect(http.StatusOK, "GET", "/spaces/"+spaceID+"/member-candidates?q=space+editor", adminT, nil)), editorID.String()) {
			t.Fatal("existing members must not be offered")
		}
	})

	t.Run("change role and remove", func(t *testing.T) {
		e.expect(http.StatusOK, "PATCH", base+"/"+editorID.String(), adminT, map[string]string{"role": "viewer"})
		e.expect(http.StatusBadRequest, "PATCH", base+"/"+editorID.String(), adminT, map[string]string{"role": "king"})
		e.expect(http.StatusNotFound, "PATCH", base+"/"+uuid.NewString(), adminT, map[string]string{"role": "viewer"})
		e.expect(http.StatusNoContent, "DELETE", base+"/"+editorID.String(), adminT, nil)
		e.expect(http.StatusNoContent, "DELETE", base+"/"+editorID.String(), adminT, nil) // idempotent
	})

	t.Run("only owners can grant, change or revoke the owner role", func(t *testing.T) {
		// an admin must not be able to self-promote (and then delete the space)
		e.expect(http.StatusForbidden, "PATCH", base+"/"+adminID.String(), adminT, map[string]string{"role": "owner"})
		e.expect(http.StatusForbidden, "POST", base, adminT, map[string]string{"userId": adminID.String(), "role": "owner"})
		e.expect(http.StatusForbidden, "PATCH", base+"/"+ownerID.String(), adminT, map[string]string{"role": "viewer"})
		e.expect(http.StatusForbidden, "DELETE", base+"/"+ownerID.String(), adminT, nil)
		e.expect(http.StatusForbidden, "DELETE", "/spaces/"+spaceID, adminT, nil)
		// but admins still manage non-owner members
		e.expect(http.StatusNotFound, "POST", base, adminT, map[string]string{"userId": uuid.NewString(), "role": "viewer"}) // reaches the FK check, not a 403
	})

	t.Run("last owner cannot be demoted, removed or overwritten", func(t *testing.T) {
		// make `ownerID` the only owner: drop the creating org admin's owner row
		e.expect(http.StatusNoContent, "DELETE", base+"/"+e.admin.String(), ownerT, nil)

		e.expect(http.StatusConflict, "PATCH", base+"/"+ownerID.String(), ownerT, map[string]string{"role": "admin"})
		e.expect(http.StatusConflict, "DELETE", base+"/"+ownerID.String(), ownerT, nil)
		e.expect(http.StatusConflict, "POST", base, ownerT, map[string]string{"userId": ownerID.String(), "role": "viewer"})

		// with a second owner the first can step down
		e.expect(http.StatusOK, "PATCH", base+"/"+adminID.String(), ownerT, map[string]string{"role": "owner"})
		e.expect(http.StatusOK, "PATCH", base+"/"+ownerID.String(), ownerT, map[string]string{"role": "admin"})
		e.expect(http.StatusConflict, "DELETE", base+"/"+adminID.String(), adminT, nil) // now the last one
	})
}

func TestGroupBasedSpaceMembership(t *testing.T) {
	e := newEnv(t)
	spaceID := e.newSpace()
	pageID := e.newPage(e.adminT, spaceID, "Group page", docWith("hi"), "")
	uid, userT := e.newUser("Group Member")
	_, otherT := e.newUser("Not In Group")

	groupName := "grp-" + uuid.NewString()
	var groupID string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO groups (name) VALUES ($1) RETURNING id::text`, groupName).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	e.exec(`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, groupID, uid)

	e.expect(http.StatusForbidden, "GET", "/pages/"+pageID, userT, nil)

	gm := "/spaces/" + spaceID + "/group-members"
	e.expect(http.StatusForbidden, "POST", gm, userT, map[string]string{"groupId": groupID, "role": "editor"})
	e.expect(http.StatusBadRequest, "POST", gm, e.adminT, map[string]string{"groupId": "nope", "role": "editor"})
	e.expect(http.StatusNotFound, "POST", gm, e.adminT, map[string]string{"groupId": uuid.NewString(), "role": "editor"})
	e.expect(http.StatusCreated, "POST", gm, e.adminT, map[string]string{"groupId": groupID, "role": "viewer"})

	t.Run("group role applies to its members only", func(t *testing.T) {
		e.expect(http.StatusOK, "GET", "/pages/"+pageID, userT, nil)
		e.expect(http.StatusForbidden, "GET", "/pages/"+pageID, otherT, nil)
		if status, _ := e.save(userT, pageID, "x", docWith("no")); status != http.StatusForbidden {
			t.Fatalf("viewer group must not edit, got %d", status)
		}
		spaces := decode[[]struct {
			ID     string `json:"id"`
			MyRole string `json:"my_role"`
		}](t, e.expect(http.StatusOK, "GET", "/spaces", userT, nil))
		if len(spaces) != 1 || spaces[0].ID != spaceID || spaces[0].MyRole != "viewer" {
			t.Fatalf("group-derived space missing from list: %+v", spaces)
		}
	})

	t.Run("highest of direct and group role wins", func(t *testing.T) {
		e.expect(http.StatusOK, "PATCH", gm+"/"+groupID, e.adminT, map[string]string{"role": "editor"})
		if status, body := e.save(userT, pageID, "Group page", docWith("edited by group")); status != http.StatusOK {
			t.Fatalf("editor group should edit, got %d: %s", status, body)
		}
		e.addMember(spaceID, uid, "viewer") // direct viewer must not lower the group's editor
		if status, _ := e.save(userT, pageID, "Group page", docWith("still editor")); status != http.StatusOK {
			t.Fatalf("direct viewer + group editor should still edit, got %d", status)
		}
		e.expect(http.StatusOK, "PATCH", "/spaces/"+spaceID+"/members/"+uid.String(), e.adminT, map[string]string{"role": "admin"})
		e.expect(http.StatusOK, "GET", "/spaces/"+spaceID+"/members", userT, nil)
	})

	t.Run("listing and candidates show the group", func(t *testing.T) {
		members := decode[[]memberJSON](t, e.expect(http.StatusOK, "GET", "/spaces/"+spaceID+"/members", e.adminT, nil))
		found := false
		for _, m := range members {
			if m.GroupID != nil && *m.GroupID == groupID && m.GroupName != nil && *m.GroupName == groupName {
				found = true
			}
		}
		if !found {
			t.Fatalf("group missing from members: %+v", members)
		}
		if !strings.Contains(string(e.expect(http.StatusOK, "GET", "/spaces/"+spaceID+"/group-candidates?q="+groupName, e.adminT, nil)), groupID) {
			t.Fatal("group candidate missing")
		}
	})

	t.Run("removing the group drops access", func(t *testing.T) {
		e.expect(http.StatusNoContent, "DELETE", "/spaces/"+spaceID+"/members/"+uid.String(), e.adminT, nil)
		e.expect(http.StatusNoContent, "DELETE", gm+"/"+groupID, e.adminT, nil)
		e.expect(http.StatusForbidden, "GET", "/pages/"+pageID, userT, nil)
	})

	t.Run("a group owner does not satisfy the last-owner guard", func(t *testing.T) {
		sp := e.newSpace()
		e.expect(http.StatusCreated, "POST", "/spaces/"+sp+"/group-members", e.adminT, map[string]string{"groupId": groupID, "role": "owner"})
		// the group is an owner, but the only direct owner still can't leave
		e.expect(http.StatusConflict, "DELETE", "/spaces/"+sp+"/members/"+e.admin.String(), e.adminT, nil)
		// group rows themselves can always be changed or removed
		e.expect(http.StatusOK, "PATCH", "/spaces/"+sp+"/group-members/"+groupID, e.adminT, map[string]string{"role": "viewer"})
		e.expect(http.StatusNoContent, "DELETE", "/spaces/"+sp+"/group-members/"+groupID, e.adminT, nil)
	})
}

func TestPageDeleteLockAndTemplates(t *testing.T) {
	e := newEnv(t)
	spaceID := e.newSpace()
	editorID, editorT := e.newUser("Page Editor")
	viewerID, viewerT := e.newUser("Page Viewer")
	e.addMember(spaceID, editorID, "editor")
	e.addMember(spaceID, viewerID, "viewer")

	t.Run("templates", func(t *testing.T) {
		tpls := decode[[]struct {
			Key     string          `json:"key"`
			Name    string          `json:"name"`
			Content json.RawMessage `json:"content"`
		}](t, e.expect(http.StatusOK, "GET", "/page-templates", viewerT, nil))
		if len(tpls) < 3 || tpls[0].Key != "blank" {
			t.Fatalf("unexpected templates: %+v", tpls)
		}
		e.expect(http.StatusUnauthorized, "GET", "/page-templates", "", nil)

		e.expect(http.StatusBadRequest, "POST", "/spaces/"+spaceID+"/pages", editorT, map[string]any{"templateKey": "nope"})
		e.expect(http.StatusBadRequest, "POST", "/spaces/"+spaceID+"/pages", editorT, map[string]any{"templateKey": "blank", "content": docWith("x")})
		e.expect(http.StatusForbidden, "POST", "/spaces/"+spaceID+"/pages", viewerT, map[string]any{"templateKey": "meeting-notes"})
		page := decode[struct {
			ID      string          `json:"id"`
			Title   string          `json:"title"`
			Content json.RawMessage `json:"content"`
		}](t, e.expect(http.StatusCreated, "POST", "/spaces/"+spaceID+"/pages", editorT, map[string]any{"templateKey": "meeting-notes"}))
		if page.Title != "Meeting notes" || !bytes.Contains(page.Content, []byte("Action items")) {
			t.Fatalf("template not applied: %+v", page)
		}
		// template text is searchable immediately (index on every create path)
		if !strings.Contains(string(e.expect(http.StatusOK, "GET", "/spaces/"+spaceID+"/search?q=agenda", editorT, nil)), page.ID) {
			t.Fatal("page created from a template must be indexed")
		}
	})

	t.Run("lock and unlock", func(t *testing.T) {
		pageID := e.newPage(editorT, spaceID, "Lockable", docWith("a"), "")
		e.expect(http.StatusForbidden, "PATCH", "/pages/"+pageID+"/lock", viewerT, map[string]bool{"locked": true})
		e.expect(http.StatusBadRequest, "PATCH", "/pages/"+pageID+"/lock", editorT, map[string]any{})
		locked := decode[struct {
			Locked bool `json:"locked"`
		}](t, e.expect(http.StatusOK, "PATCH", "/pages/"+pageID+"/lock", editorT, map[string]bool{"locked": true}))
		if !locked.Locked {
			t.Fatal("expected locked")
		}
		if status, _ := e.save(editorT, pageID, "Lockable", docWith("b")); status != http.StatusForbidden {
			t.Fatalf("locked page must refuse writes, got %d", status)
		}
		e.expect(http.StatusForbidden, "DELETE", "/pages/"+pageID, editorT, nil) // unlock first
		// a locked descendant also blocks deleting an unlocked ancestor
		parent := e.newPage(editorT, spaceID, "Unlocked parent", docWith("p"), "")
		child := e.newPage(editorT, spaceID, "Locked child", docWith("c"), parent)
		e.expect(http.StatusOK, "PATCH", "/pages/"+child+"/lock", editorT, map[string]bool{"locked": true})
		e.expect(http.StatusForbidden, "DELETE", "/pages/"+parent, editorT, nil)
		e.expect(http.StatusOK, "PATCH", "/pages/"+child+"/lock", editorT, map[string]bool{"locked": false})
		e.expect(http.StatusNoContent, "DELETE", "/pages/"+parent, editorT, nil)
		e.expect(http.StatusOK, "PATCH", "/pages/"+pageID+"/lock", editorT, map[string]bool{"locked": false})
		if status, body := e.save(editorT, pageID, "Lockable", docWith("b")); status != http.StatusOK {
			t.Fatalf("unlocked save: %d %s", status, body)
		}
	})

	t.Run("title-only update leaves content alone", func(t *testing.T) {
		pageID := e.newPage(editorT, spaceID, "Old name", docWith("keep me"), "")
		out := decode[struct {
			Title   string          `json:"title"`
			Content json.RawMessage `json:"content"`
		}](t, e.expect(http.StatusOK, "PUT", "/pages/"+pageID+"/content", editorT, map[string]string{"title": "New name"}))
		if out.Title != "New name" || !bytes.Contains(out.Content, []byte("keep me")) {
			t.Fatalf("rename must keep content: %+v", out)
		}
		e.expect(http.StatusBadRequest, "PUT", "/pages/"+pageID+"/content", editorT, map[string]any{})
		e.expect(http.StatusBadRequest, "PUT", "/pages/"+pageID+"/content", editorT, map[string]any{"title": ""})
		// content without a title keeps the existing title
		out = decode[struct {
			Title   string          `json:"title"`
			Content json.RawMessage `json:"content"`
		}](t, e.expect(http.StatusOK, "PUT", "/pages/"+pageID+"/content", editorT, map[string]any{"content": docWith("fresh words")}))
		if out.Title != "New name" || !bytes.Contains(out.Content, []byte("fresh words")) {
			t.Fatalf("unexpected: %+v", out)
		}
	})

	t.Run("delete removes the subtree, attachments and blobs", func(t *testing.T) {
		parent := e.newPage(editorT, spaceID, "Doomed parent", docWith("p"), "")
		child := e.newPage(editorT, spaceID, "Doomed child", docWith("c"), parent)
		status, body := uploadAttachmentAs(t, e.ts.Client(), e.ts.URL, editorT, child, "note.txt", "text/plain", []byte("hello blob"))
		if status != http.StatusCreated {
			t.Fatalf("upload: %d %s", status, body)
		}
		att := decode[idOnly](t, body)

		e.expect(http.StatusForbidden, "DELETE", "/pages/"+parent, viewerT, nil)
		e.expect(http.StatusNotFound, "DELETE", "/pages/"+uuid.NewString(), editorT, nil)
		e.expect(http.StatusNoContent, "DELETE", "/pages/"+parent, editorT, nil)
		e.expect(http.StatusNotFound, "GET", "/pages/"+parent, e.adminT, nil)
		e.expect(http.StatusNotFound, "GET", "/pages/"+child, e.adminT, nil)
		e.expect(http.StatusNotFound, "GET", "/attachments/"+att.ID, e.adminT, nil)
	})

	t.Run("edit-link guests cannot delete or lock", func(t *testing.T) {
		pageID := e.newPage(editorT, spaceID, "Shared", docWith("s"), "")
		_, linkTok := createShareLink(t, e.ts.Client(), e.ts.URL, editorT, pageID, map[string]string{"permission": "edit"})
		guestT := redeemShareLink(t, e.ts.Client(), e.ts.URL, linkTok, "Guest")
		e.expect(http.StatusForbidden, "DELETE", "/pages/"+pageID, guestT, nil)
		e.expect(http.StatusForbidden, "PATCH", "/pages/"+pageID+"/lock", guestT, map[string]bool{"locked": true})
	})
}

func TestSpaceUpdateAndDelete(t *testing.T) {
	e := newEnv(t)
	spaceID := e.newSpace()
	adminID, adminT := e.newUser("S Admin")
	editorID, editorT := e.newUser("S Editor")
	e.addMember(spaceID, adminID, "admin")
	e.addMember(spaceID, editorID, "editor")

	type spaceOut struct {
		Name   string  `json:"name"`
		Icon   *string `json:"icon"`
		MaxCnt *int    `json:"history_max_count"`
		MaxAge *int    `json:"history_max_age_days"`
		MyRole string  `json:"my_role"`
	}
	e.expect(http.StatusForbidden, "PATCH", "/spaces/"+spaceID, editorT, map[string]string{"name": "Nope"})
	got := decode[spaceOut](t, e.expect(http.StatusOK, "PATCH", "/spaces/"+spaceID, adminT, map[string]any{
		"name": "Renamed", "icon": "book", "historyMaxCount": 7, "historyMaxAgeDays": 30,
	}))
	if got.Name != "Renamed" || got.Icon == nil || *got.Icon != "book" || got.MaxCnt == nil || *got.MaxCnt != 7 || *got.MaxAge != 30 || got.MyRole != "admin" {
		t.Fatalf("unexpected %+v", got)
	}
	// absent = unchanged; null = clear
	got = decode[spaceOut](t, e.expect(http.StatusOK, "PATCH", "/spaces/"+spaceID, adminT, []byte(`{"icon":null,"historyMaxCount":null}`)))
	if got.Name != "Renamed" || got.Icon != nil || got.MaxCnt != nil || got.MaxAge == nil || *got.MaxAge != 30 {
		t.Fatalf("null/absent semantics wrong: %+v", got)
	}
	e.expect(http.StatusBadRequest, "PATCH", "/spaces/"+spaceID, adminT, map[string]any{"name": "  "})
	e.expect(http.StatusBadRequest, "PATCH", "/spaces/"+spaceID, adminT, map[string]any{"historyMaxCount": 0})
	e.expect(http.StatusNotFound, "PATCH", "/spaces/"+uuid.NewString(), e.adminT, map[string]any{"name": "x"})

	// delete: owner (or org admin) only
	pageID := e.newPage(e.adminT, spaceID, "Doomed", docWith("x"), "")
	status, body := uploadAttachmentAs(t, e.ts.Client(), e.ts.URL, e.adminT, pageID, "a.txt", "text/plain", []byte("blob"))
	if status != http.StatusCreated {
		t.Fatalf("upload: %d %s", status, body)
	}
	e.expect(http.StatusForbidden, "DELETE", "/spaces/"+spaceID, adminT, nil)
	e.expect(http.StatusForbidden, "DELETE", "/spaces/"+spaceID, editorT, nil)
	e.expect(http.StatusNoContent, "DELETE", "/spaces/"+spaceID, e.adminT, nil)
	e.expect(http.StatusNotFound, "GET", "/spaces/"+spaceID, e.adminT, nil)
	e.expect(http.StatusNotFound, "GET", "/pages/"+pageID, e.adminT, nil)
	e.expect(http.StatusNotFound, "DELETE", "/spaces/"+spaceID, e.adminT, nil)
}
