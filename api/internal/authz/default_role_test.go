package authz

import (
	"testing"

	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
)

func TestWithDefaultRole(t *testing.T) {
	t.Cleanup(func() { SetDefaultMemberRole("") })
	owner, viewer := db.SpaceRoleOwner, db.SpaceRoleViewer

	SetDefaultMemberRole("")
	if WithDefaultRole(nil) != nil {
		t.Fatal("no default: non-member stays without a role")
	}

	SetDefaultMemberRole("editor")
	if got := WithDefaultRole(nil); got == nil || *got != db.SpaceRoleEditor {
		t.Fatalf("non-member should be editor, got %v", got)
	}
	if got := WithDefaultRole(&viewer); *got != db.SpaceRoleEditor {
		t.Fatalf("viewer membership should be raised to editor, got %v", *got)
	}
	if got := WithDefaultRole(&owner); *got != db.SpaceRoleOwner {
		t.Fatalf("owner must not be lowered, got %v", *got)
	}
}
