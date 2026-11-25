//go:build integration

package integrationtest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TaliaMarine/little-doc-editor/api/internal/accountsapi"
)

type inviteJSON struct {
	ID     string  `json:"id"`
	Email  *string `json:"email"`
	Status string  `json:"status"`
	Token  string  `json:"token"`
}

func TestInviteLifecycle(t *testing.T) {
	e := newEnv(t)
	_, outsiderT := e.newUser("Not Admin")

	t.Run("only org admins manage invites", func(t *testing.T) {
		e.expect(http.StatusForbidden, "POST", "/invites", outsiderT, map[string]any{})
		e.expect(http.StatusForbidden, "GET", "/invites", outsiderT, nil)
		e.expect(http.StatusUnauthorized, "POST", "/invites", "", map[string]any{})
	})

	email := "invitee-" + uuid.NewString() + "@Example.com"
	inv := decode[inviteJSON](t, e.expect(http.StatusCreated, "POST", "/invites", e.adminT, map[string]any{"email": email}))
	if inv.Token == "" || inv.Status != "pending" || inv.Email == nil || *inv.Email != strings.ToLower(email) {
		t.Fatalf("unexpected invite: %+v", inv)
	}

	t.Run("token is stored only as a hash and never listed again", func(t *testing.T) {
		var stored string
		if err := e.pool.QueryRow(context.Background(), `SELECT token_hash FROM invites WHERE id = $1`, inv.ID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored == inv.Token || stored != accountsapi.HashToken(inv.Token) {
			t.Fatalf("token_hash must be the SHA-256 of the token, got %q", stored)
		}
		list := e.expect(http.StatusOK, "GET", "/invites", e.adminT, nil)
		if strings.Contains(string(list), inv.Token) || strings.Contains(string(list), `"token"`) {
			t.Fatalf("invite list leaked a token: %s", list)
		}
	})

	t.Run("public resolve", func(t *testing.T) {
		out := decode[struct {
			Email *string `json:"email"`
		}](t, e.expect(http.StatusOK, "GET", "/invites/"+inv.Token, "", nil))
		if out.Email == nil || *out.Email != strings.ToLower(email) {
			t.Fatalf("resolve should prefill the bound email, got %+v", out)
		}
		e.expect(http.StatusNotFound, "GET", "/invites/not-a-real-token", "", nil)
	})

	t.Run("redeem validates input", func(t *testing.T) {
		e.expect(http.StatusBadRequest, "POST", "/invites/"+inv.Token+"/redeem", "", map[string]string{"displayName": "", "password": "long-enough-password"})
		e.expect(http.StatusBadRequest, "POST", "/invites/"+inv.Token+"/redeem", "", map[string]string{"displayName": "Ok", "password": "short"})
	})

	var session struct {
		Token      string `json:"token"`
		IsOrgAdmin bool   `json:"isOrgAdmin"`
	}
	t.Run("redeem creates the account (argon2id) and signs in", func(t *testing.T) {
		session = decode[struct {
			Token      string `json:"token"`
			IsOrgAdmin bool   `json:"isOrgAdmin"`
		}](t, e.expect(http.StatusCreated, "POST", "/invites/"+inv.Token+"/redeem", "", map[string]string{
			"displayName": "New Person", "password": "a-long-passphrase!", "email": "ignored@example.com",
		}))
		if session.Token == "" || session.IsOrgAdmin {
			t.Fatalf("unexpected session %+v", session)
		}
		e.expect(http.StatusOK, "GET", "/auth/me", session.Token, nil)

		var hash, gotEmail string
		if err := e.pool.QueryRow(context.Background(), `SELECT password_hash, email FROM users WHERE display_name = 'New Person' AND email = $1`, strings.ToLower(email)).Scan(&hash, &gotEmail); err != nil {
			t.Fatalf("user not created with the invite's email: %v", err)
		}
		if !strings.HasPrefix(hash, "$argon2id$") {
			t.Fatalf("password must be argon2id, got %q", hash)
		}
		// and a normal password login works
		e.expect(http.StatusOK, "POST", "/auth/login", "", map[string]string{"email": strings.ToLower(email), "password": "a-long-passphrase!"})
	})

	t.Run("an invite is single use", func(t *testing.T) {
		e.expect(http.StatusGone, "POST", "/invites/"+inv.Token+"/redeem", "", map[string]string{"displayName": "Again", "password": "a-long-passphrase!"})
		e.expect(http.StatusGone, "GET", "/invites/"+inv.Token, "", nil)
	})

	t.Run("concurrent redemptions: exactly one wins", func(t *testing.T) {
		race := decode[inviteJSON](t, e.expect(http.StatusCreated, "POST", "/invites", e.adminT, map[string]any{}))
		results := make(chan int, 6)
		for i := 0; i < 6; i++ {
			go func(i int) {
				status, _ := e.do("POST", "/invites/"+race.Token+"/redeem", "", map[string]string{
					"displayName": "Racer", "password": "a-long-passphrase!", "email": "racer-" + uuid.NewString() + "@example.com",
				})
				results <- status
			}(i)
		}
		created := 0
		for i := 0; i < 6; i++ {
			if <-results == http.StatusCreated {
				created++
			}
		}
		if created != 1 {
			t.Fatalf("expected exactly one account from one invite, got %d", created)
		}
	})

	t.Run("duplicate email conflicts and does not burn the invite", func(t *testing.T) {
		dup := decode[inviteJSON](t, e.expect(http.StatusCreated, "POST", "/invites", e.adminT, map[string]any{}))
		e.expect(http.StatusConflict, "POST", "/invites/"+dup.Token+"/redeem", "", map[string]string{
			"displayName": "Dup", "password": "a-long-passphrase!", "email": strings.ToUpper(email),
		})
		e.expect(http.StatusOK, "GET", "/invites/"+dup.Token, "", nil)
		e.expect(http.StatusBadRequest, "POST", "/invites/"+dup.Token+"/redeem", "", map[string]string{"displayName": "Dup", "password": "a-long-passphrase!", "email": "not-an-email"})
		// creating an invite for an existing account is refused up front
		e.expect(http.StatusConflict, "POST", "/invites", e.adminT, map[string]any{"email": email})
	})

	t.Run("grantsOrgAdmin", func(t *testing.T) {
		adm := decode[inviteJSON](t, e.expect(http.StatusCreated, "POST", "/invites", e.adminT, map[string]any{"grantsOrgAdmin": true}))
		s := decode[struct {
			IsOrgAdmin bool `json:"isOrgAdmin"`
		}](t, e.expect(http.StatusCreated, "POST", "/invites/"+adm.Token+"/redeem", "", map[string]string{
			"displayName": "Another Admin", "password": "a-long-passphrase!", "email": "adm-" + uuid.NewString() + "@example.com",
		}))
		if !s.IsOrgAdmin {
			t.Fatal("invite with grantsOrgAdmin must create an org admin")
		}
	})

	t.Run("revoke, expiry and listing statuses", func(t *testing.T) {
		rev := decode[inviteJSON](t, e.expect(http.StatusCreated, "POST", "/invites", e.adminT, map[string]any{}))
		e.expect(http.StatusNoContent, "DELETE", "/invites/"+rev.ID, e.adminT, nil)
		e.expect(http.StatusGone, "GET", "/invites/"+rev.Token, "", nil)
		e.expect(http.StatusGone, "POST", "/invites/"+rev.Token+"/redeem", "", map[string]string{"displayName": "X", "password": "a-long-passphrase!", "email": "r@example.com"})
		e.expect(http.StatusConflict, "DELETE", "/invites/"+rev.ID, e.adminT, nil) // no longer pending
		e.expect(http.StatusNotFound, "DELETE", "/invites/"+uuid.NewString(), e.adminT, nil)

		exp := decode[inviteJSON](t, e.expect(http.StatusCreated, "POST", "/invites", e.adminT, map[string]any{}))
		e.exec(`UPDATE invites SET expires_at = now() - interval '1 minute' WHERE id = $1`, exp.ID)
		e.expect(http.StatusGone, "GET", "/invites/"+exp.Token, "", nil)

		statuses := map[string]string{}
		var list []inviteJSON = decode[[]inviteJSON](t, e.expect(http.StatusOK, "GET", "/invites", e.adminT, nil))
		for _, i := range list {
			statuses[i.ID] = i.Status
		}
		if statuses[rev.ID] != "revoked" || statuses[exp.ID] != "expired" || statuses[inv.ID] != "redeemed" {
			t.Fatalf("unexpected statuses: %v", statuses)
		}
	})

	t.Run("expiresAt bounds", func(t *testing.T) {
		e.expect(http.StatusBadRequest, "POST", "/invites", e.adminT, map[string]any{"expiresAt": time.Now().Add(-time.Hour).Format(time.RFC3339)})
		e.expect(http.StatusBadRequest, "POST", "/invites", e.adminT, map[string]any{"expiresAt": time.Now().Add(200 * 24 * time.Hour).Format(time.RFC3339)})
		e.expect(http.StatusCreated, "POST", "/invites", e.adminT, map[string]any{"expiresAt": time.Now().Add(time.Hour).Format(time.RFC3339)})
	})
}

func TestUserDirectoryAndDisable(t *testing.T) {
	e := newEnv(t)
	uniq := "Dirtarget" + strings.ReplaceAll(uuid.NewString(), "-", "")
	uid, userT := e.newUser(uniq)
	_, outsiderT := e.newUser("Other")

	type userRow struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		Disabled bool   `json:"disabled"`
	}
	e.expect(http.StatusForbidden, "GET", "/users", outsiderT, nil)
	users := decode[[]userRow](t, e.expect(http.StatusOK, "GET", "/users?q="+uniq+"&limit=5", e.adminT, nil))
	if len(users) != 1 || users[0].ID != uid.String() {
		t.Fatalf("q filter should find exactly the target, got %+v", users)
	}
	if strings.Contains(string(e.expect(http.StatusOK, "GET", "/users", e.adminT, nil)), "password") {
		t.Fatal("user list must not expose password hashes")
	}

	e.expect(http.StatusOK, "GET", "/auth/me", userT, nil)
	e.expect(http.StatusForbidden, "PATCH", "/users/"+uid.String(), outsiderT, map[string]bool{"disabled": true})
	e.expect(http.StatusBadRequest, "PATCH", "/users/"+uid.String(), e.adminT, map[string]any{})
	got := decode[userRow](t, e.expect(http.StatusOK, "PATCH", "/users/"+uid.String(), e.adminT, map[string]bool{"disabled": true}))
	if !got.Disabled {
		t.Fatal("expected disabled")
	}
	e.expect(http.StatusUnauthorized, "GET", "/auth/me", userT, nil) // takes effect immediately
	e.expect(http.StatusOK, "PATCH", "/users/"+uid.String(), e.adminT, map[string]bool{"disabled": false})
	e.expect(http.StatusOK, "GET", "/auth/me", userT, nil)
	e.expect(http.StatusConflict, "PATCH", "/users/"+e.admin.String(), e.adminT, map[string]bool{"disabled": true}) // self
	e.expect(http.StatusNotFound, "PATCH", "/users/"+uuid.NewString(), e.adminT, map[string]bool{"disabled": true})
}
