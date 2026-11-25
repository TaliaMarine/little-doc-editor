package oidcapi

import (
	"testing"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
)

func newTestHandler() *Handler {
	return &Handler{cfg: config.Config{OIDCAdminGroup: "homelab-admins", OIDCGroupsClaim: "groups"}}
}

func TestProfileFromClaims_AdminGroup(t *testing.T) {
	h := newTestHandler()
	p := h.profileFromClaims("sub1", map[string]any{
		"email": "Ada@Example.com", "name": "Ada L", "preferred_username": "ada",
		"picture": "https://x/y.png", "groups": []any{"staff", "homelab-admins"},
	})
	if !p.IsAdmin {
		t.Fatal("member of homelab-admins must be admin")
	}
	if p.Email != "ada@example.com" || p.DisplayName != "Ada L" || p.Username != "ada" || p.AvatarURL == "" || len(p.Groups) != 2 {
		t.Fatalf("unexpected profile: %+v", p)
	}
}

func TestProfileFromClaims_OtherUserIsNotAdmin(t *testing.T) {
	h := newTestHandler()
	p := h.profileFromClaims("sub2", map[string]any{"email": "bob@example.com", "groups": []any{"staff"}})
	if p.IsAdmin {
		t.Fatal("non-members must not be admin")
	}
	if p.DisplayName != "bob@example.com" {
		t.Fatalf("display name should fall back to email, got %q", p.DisplayName)
	}
}

func TestProfileFromClaims_NoClaimsFallsBackToSubject(t *testing.T) {
	h := newTestHandler()
	p := h.profileFromClaims("sub3", map[string]any{})
	if p.DisplayName != "sub3" || p.Email != "sub3@oidc.invalid" || p.IsAdmin || p.Groups == nil {
		t.Fatalf("unexpected profile: %+v", p)
	}
}

func TestProfileFromClaims_AdminGroupUnsetNeverAdmin(t *testing.T) {
	h := &Handler{cfg: config.Config{OIDCGroupsClaim: "groups"}}
	if h.profileFromClaims("s", map[string]any{"groups": []any{"homelab-admins"}}).IsAdmin {
		t.Fatal("no admin group configured: nobody is promoted")
	}
}

func TestProfileFromClaims_UnverifiedEmailIsNotTrusted(t *testing.T) {
	h := newTestHandler()
	for _, v := range []any{false, "false"} {
		p := h.profileFromClaims("sub9", map[string]any{"email": "victim@example.com", "email_verified": v})
		if p.Email != "sub9@oidc.invalid" {
			t.Fatalf("unverified email must not be stored, got %q", p.Email)
		}
	}
	p := h.profileFromClaims("sub9", map[string]any{"email": "ok@example.com", "email_verified": true})
	if p.Email != "ok@example.com" {
		t.Fatalf("verified email kept, got %q", p.Email)
	}
	p = h.profileFromClaims("sub9", map[string]any{"email": "ok@example.com"})
	if p.Email != "ok@example.com" {
		t.Fatalf("absent claim is trusted, got %q", p.Email)
	}
}
