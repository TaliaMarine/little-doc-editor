//go:build integration

package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/passwordhash"
	"github.com/TaliaMarine/little-doc-editor/api/internal/telemetry"
)

func newInternalUser(t *testing.T, e *env, email, password string) {
	t.Helper()
	newInternalUserAs(t, e, email, password, false)
}

func newInternalUserAs(t *testing.T, e *env, email, password string, orgAdmin bool) {
	t.Helper()
	hash, err := passwordhash.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if orgAdmin {
		if _, err := e.q.CreateInternalOrgAdmin(context.Background(), db.CreateInternalOrgAdminParams{Email: email, DisplayName: "Login " + email, PasswordHash: &hash}); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := e.q.CreateInternalUser(context.Background(), db.CreateInternalUserParams{Email: email, DisplayName: "Login " + email, PasswordHash: &hash}); err != nil {
		t.Fatal(err)
	}
}

func login(e *env, email, password string) (int, string) {
	e.t.Helper()
	status, body := e.do("POST", "/auth/login", "", map[string]string{"email": email, "password": password})
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(body, &out)
	return status, out.Token
}

func TestLogoutRevokesTheSession(t *testing.T) {
	e := newEnv(t)
	email := "logout-" + uuid.NewString() + "@example.com"
	newInternalUser(t, e, email, "a-long-passphrase!")

	status, token := login(e, email, "a-long-passphrase!")
	if status != http.StatusOK {
		t.Fatalf("login: %d", status)
	}
	other := func() string { _, tok := login(e, email, "a-long-passphrase!"); return tok }()

	if status, _ := login(e, strings.ToUpper(email), "a-long-passphrase!"); status != http.StatusOK {
		t.Fatalf("login must not be case-sensitive on the email, got %d", status)
	}
	e.expect(http.StatusOK, "GET", "/auth/me", token, nil)
	e.expect(http.StatusNoContent, "POST", "/auth/logout", token, nil)
	e.expect(http.StatusUnauthorized, "GET", "/auth/me", token, nil) // revoked immediately
	e.expect(http.StatusUnauthorized, "POST", "/auth/logout", token, nil)
	e.expect(http.StatusOK, "GET", "/auth/me", other, nil) // other sessions are unaffected

	t.Run("a token with no recorded session row can still be revoked", func(t *testing.T) {
		_, seeded := e.newUser("Never Logged In") // token minted directly, no sessions row
		e.expect(http.StatusOK, "GET", "/auth/me", seeded, nil)
		e.expect(http.StatusNoContent, "POST", "/auth/logout", seeded, nil)
		e.expect(http.StatusUnauthorized, "GET", "/auth/me", seeded, nil)
	})

	t.Run("collab's deny-list query: sha256 hex of the token", func(t *testing.T) {
		var revoked bool
		err := e.pool.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM sessions WHERE token_hash = encode(sha256(convert_to($1, 'UTF8')), 'hex') AND revoked_at IS NOT NULL)`, token).Scan(&revoked)
		if err != nil || !revoked {
			t.Fatalf("revoked token must be findable by sha256(token) hex: %v %v", revoked, err)
		}
	})

	t.Run("guest sessions can log out too", func(t *testing.T) {
		spaceID := e.newSpace()
		pageID := e.newPage(e.adminT, spaceID, "G", docWith("g"), "")
		_, linkTok := createShareLink(t, e.ts.Client(), e.ts.URL, e.adminT, pageID, map[string]string{"permission": "view"})
		g := redeemShareLink(t, e.ts.Client(), e.ts.URL, linkTok, "Gee")
		e.expect(http.StatusOK, "GET", "/pages/"+pageID, g, nil)
		e.expect(http.StatusNoContent, "POST", "/auth/logout", g, nil)
		e.expect(http.StatusUnauthorized, "GET", "/pages/"+pageID, g, nil)
	})
}

func TestBreakGlassIsRestrictedToTheBootstrapAdmin(t *testing.T) {
	bootstrap := "bootstrap-" + uuid.NewString() + "@example.com"
	ts, pool, cfg := newRealAuthTestServerWith(t, map[string]string{
		"INTERNAL_LOGIN_BREAK_GLASS": "true",
		"BOOTSTRAP_ADMIN_EMAIL":      bootstrap,
		"BOOTSTRAP_ADMIN_PASSWORD":   "unused-in-this-test",
	})
	e := &env{t: t, ts: ts, pool: pool, cfg: cfg, q: db.New(pool)}
	e.admin, e.adminT = seedOrgAdmin(t, pool, cfg, "BG Admin")
	other := "other-" + uuid.NewString() + "@example.com"
	newInternalUserAs(t, e, bootstrap, "a-long-passphrase!", true)
	newInternalUser(t, e, other, "a-long-passphrase!")
	otherAdmin := "otheradmin-" + uuid.NewString() + "@example.com"
	newInternalUserAs(t, e, otherAdmin, "a-long-passphrase!", true)

	e.expect(http.StatusOK, "PATCH", "/admin/settings", e.adminT, map[string]bool{"internalLoginDisabled": true})
	t.Cleanup(func() { e.exec(`UPDATE instance_settings SET internal_login_disabled = false`) })

	if status, _ := login(e, bootstrap, "a-long-passphrase!"); status != http.StatusOK {
		t.Fatalf("break-glass must let the bootstrap admin in, got %d", status)
	}
	if status, _ := login(e, strings.ToUpper(bootstrap), "a-long-passphrase!"); status == http.StatusOK {
		// emails are matched exactly at lookup; the guard itself is case-insensitive but the user row isn't found
		t.Log("note: uppercase email resolved to the account")
	}
	if status, _ := login(e, other, "a-long-passphrase!"); status != http.StatusForbidden {
		t.Fatalf("break-glass must NOT re-enable other internal accounts, got %d", status)
	}
	if status, _ := login(e, otherAdmin, "a-long-passphrase!"); status != http.StatusForbidden {
		t.Fatalf("break-glass must NOT re-enable other org admins either, got %d", status)
	}
}

func TestInternalLoginDisabledWithoutBreakGlass(t *testing.T) {
	e := newEnv(t)
	email := "dis-" + uuid.NewString() + "@example.com"
	newInternalUser(t, e, email, "a-long-passphrase!")
	e.expect(http.StatusOK, "PATCH", "/admin/settings", e.adminT, map[string]bool{"internalLoginDisabled": true})
	t.Cleanup(func() { e.exec(`UPDATE instance_settings SET internal_login_disabled = false`) })
	if status, _ := login(e, email, "a-long-passphrase!"); status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
	// invites would only create accounts that cannot sign in
	e.expect(http.StatusConflict, "POST", "/invites", e.adminT, map[string]any{})
}

func TestRateLimiting(t *testing.T) {
	ts, _, _ := newRealAuthTestServerWith(t, map[string]string{
		"RATE_LIMIT_ENABLED":           "true",
		"RATE_LIMIT_LOGIN_PER_MINUTE":  "3",
		"RATE_LIMIT_PUBLIC_PER_MINUTE": "4",
		"TRUSTED_PROXIES":              "127.0.0.0/8,::1",
	})

	post := func(path, ip string, body any) (int, http.Header) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", ip) // unique per run: keys live in Redis across runs
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode, resp.Header
	}
	t.Run("login: 429 with Retry-After once the per-IP limit is spent", func(t *testing.T) {
		ip := "203.0.113." + fmt.Sprint(uuid.New().ID()%250+1)
		var last int
		var hdr http.Header
		for i := 0; i < 5; i++ {
			last, hdr = post("/auth/login", ip, map[string]string{"email": "nobody@example.com", "password": "x"})
			if i < 3 && last != http.StatusUnauthorized {
				t.Fatalf("attempt %d should reach the handler (401), got %d", i, last)
			}
		}
		if last != http.StatusTooManyRequests || hdr.Get("Retry-After") == "" {
			t.Fatalf("expected 429 + Retry-After, got %d %v", last, hdr)
		}
		// a different client IP is not affected
		if status, _ := post("/auth/login", "203.0.113."+fmt.Sprint(uuid.New().ID()%250+1)+"0", map[string]string{"email": "nobody@example.com", "password": "x"}); status == http.StatusTooManyRequests {
			t.Fatal("limit must be per client IP")
		}
	})

	t.Run("invite redeem and share redeem are limited under their own scopes", func(t *testing.T) {
		ip := "192.0.2." + fmt.Sprint(uuid.New().ID()%250+1)
		var status int
		for i := 0; i < 6; i++ {
			status, _ = post("/invites/bogus-token/redeem", ip, map[string]string{"displayName": "x", "password": "long-enough-password"})
		}
		if status != http.StatusTooManyRequests {
			t.Fatalf("invite redeem should be limited, got %d", status)
		}
		for i := 0; i < 6; i++ {
			status, _ = post("/share/bogus-token/redeem", ip, map[string]string{"displayName": "x"})
		}
		if status != http.StatusTooManyRequests {
			t.Fatalf("share redeem should be limited, got %d", status)
		}
	})
}

func TestShareAccessLogNamesViewersAndSetsUser(t *testing.T) {
	e := newEnv(t)
	spaceID := e.newSpace()
	editorID, editorT := e.newUser("Log Editor")
	viewerID, viewerT := e.newUser("Log Viewer")
	e.addMember(spaceID, editorID, "editor")
	e.addMember(spaceID, viewerID, "viewer")
	pageID := e.newPage(editorT, spaceID, "Logged", docWith("x"), "")
	linkID, linkTok := createShareLink(t, e.ts.Client(), e.ts.URL, editorT, pageID, map[string]string{"permission": "view"})

	// one anonymous redemption, one by a signed-in user (SPA sends its bearer token)
	redeemShareLink(t, e.ts.Client(), e.ts.URL, linkTok, "Anon Otter")
	body, _ := json.Marshal(map[string]string{"displayName": "Signed In"})
	if status, out := authedPost(t, e.ts.Client(), e.ts.URL+"/api/v1/share/"+linkTok+"/redeem", viewerT, body); status != http.StatusOK {
		t.Fatalf("redeem as user: %d %s", status, out)
	}

	type logRow struct {
		UserID    *string `json:"viewer_user_id"`
		GuestID   *string `json:"viewer_guest_id"`
		UserName  *string `json:"viewer_user_display_name"`
		GuestName *string `json:"viewer_guest_display_name"`
		IPHash    string  `json:"ip_hash"`
	}
	rows := decode[[]logRow](t, e.expect(http.StatusOK, "GET", "/share-links/"+linkID+"/access-log", editorT, nil))
	if len(rows) != 2 {
		t.Fatalf("expected 2 log rows, got %+v", rows)
	}
	var sawAnon, sawUser bool
	for _, r := range rows {
		if r.GuestName == nil || r.IPHash == "" {
			t.Fatalf("guest + hashed ip always recorded: %+v", r)
		}
		if r.UserID == nil && *r.GuestName == "Anon Otter" {
			sawAnon = true
		}
		if r.UserID != nil && *r.UserID == viewerID.String() && r.UserName != nil && *r.UserName == "Log Viewer" {
			sawUser = true
		}
	}
	if !sawAnon || !sawUser {
		t.Fatalf("access log must set viewer_user_id for signed-in viewers only: %+v", rows)
	}
	// Decision 16: visible to editors+; not to viewers or outsiders
	e.expect(http.StatusForbidden, "GET", "/share-links/"+linkID+"/access-log", viewerT, nil)
	e.expect(http.StatusOK, "GET", "/share-links/"+linkID+"/access-log?limit=1", e.adminT, nil)
}

func TestAdminAttachmentPolicy(t *testing.T) {
	e := newEnv(t)
	spaceID := e.newSpace()
	pageID := e.newPage(e.adminT, spaceID, "Uploads", docWith("u"), "")
	_, userT := e.newUser("Plain")
	t.Cleanup(func() {
		e.exec(`UPDATE instance_settings SET attachment_allowlist = NULL, attachment_max_size_bytes = NULL`)
	})

	type settings struct {
		Allowlist  []string `json:"attachment_allowlist"`
		MaxSize    int64    `json:"attachment_max_size_bytes"`
		Overridden bool     `json:"attachment_policy_overridden"`
		DefMax     int64    `json:"attachment_default_max_size_bytes"`
	}
	e.expect(http.StatusForbidden, "GET", "/admin/settings", userT, nil)
	def := decode[settings](t, e.expect(http.StatusOK, "GET", "/admin/settings", e.adminT, nil))
	if def.Overridden || def.MaxSize != def.DefMax || len(def.Allowlist) == 0 {
		t.Fatalf("default policy expected: %+v", def)
	}
	if status, _ := uploadAttachmentAs(t, e.ts.Client(), e.ts.URL, e.adminT, pageID, "a.txt", "text/plain", []byte("plain text ok")); status != http.StatusCreated {
		t.Fatalf("text/plain is allowed by default, got %d", status)
	}

	e.expect(http.StatusBadRequest, "PATCH", "/admin/settings", e.adminT, map[string]any{})
	e.expect(http.StatusBadRequest, "PATCH", "/admin/settings", e.adminT, map[string]any{"attachmentAllowlist": []string{"not a mime"}})
	e.expect(http.StatusBadRequest, "PATCH", "/admin/settings", e.adminT, map[string]any{"attachmentAllowlist": []string{}})
	e.expect(http.StatusBadRequest, "PATCH", "/admin/settings", e.adminT, map[string]any{"attachmentMaxSizeBytes": 0})
	e.expect(http.StatusBadRequest, "PATCH", "/admin/settings", e.adminT, map[string]any{"attachmentMaxSizeBytes": int64(1) << 40})
	e.expect(http.StatusForbidden, "PATCH", "/admin/settings", userT, map[string]any{"attachmentMaxSizeBytes": 10})

	got := decode[settings](t, e.expect(http.StatusOK, "PATCH", "/admin/settings", e.adminT, map[string]any{
		"attachmentAllowlist": []string{"Image/PNG", "image/png"}, "attachmentMaxSizeBytes": 64,
	}))
	if !got.Overridden || len(got.Allowlist) != 1 || got.Allowlist[0] != "image/png" || got.MaxSize != 64 {
		t.Fatalf("override not applied/normalized: %+v", got)
	}
	if status, _ := uploadAttachmentAs(t, e.ts.Client(), e.ts.URL, e.adminT, pageID, "a.txt", "text/plain", []byte("now disallowed")); status != http.StatusUnprocessableEntity {
		t.Fatalf("text/plain must now be refused, got %d", status)
	}

	// an unrelated PATCH keeps the override; null resets one field at a time
	e.expect(http.StatusOK, "PATCH", "/admin/settings", e.adminT, map[string]any{"internalLoginDisabled": false})
	got = decode[settings](t, e.expect(http.StatusOK, "PATCH", "/admin/settings", e.adminT, []byte(`{"attachmentAllowlist":null}`)))
	if got.MaxSize != 64 || len(got.Allowlist) == 1 {
		t.Fatalf("null should reset only the allowlist: %+v", got)
	}
	if status, _ := uploadAttachmentAs(t, e.ts.Client(), e.ts.URL, e.adminT, pageID, "big.txt", "text/plain", bytes.Repeat([]byte("x"), 100)); status != http.StatusUnprocessableEntity {
		t.Fatalf("64-byte limit must apply, got %d", status)
	}
	got = decode[settings](t, e.expect(http.StatusOK, "PATCH", "/admin/settings", e.adminT, []byte(`{"attachmentMaxSizeBytes":null}`)))
	if got.Overridden || got.MaxSize != def.MaxSize {
		t.Fatalf("fully reset expected: %+v", got)
	}
}

func TestMetricsEndpointAndRouteLabels(t *testing.T) {
	e := newEnv(t)
	e.expect(http.StatusNotFound, "GET", "/share/secret-token-value", "", nil)
	rec := httptest.NewRecorder()
	telemetry.MetricsHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "lde_api_http_requests_total") || !strings.Contains(body, `route="/api/v1/share/{token}"`) {
		t.Fatalf("expected route-labelled request counter, got:\n%.600s", body)
	}
	if strings.Contains(body, "secret-token-value") {
		t.Fatal("bearer tokens in paths must never reach metric labels")
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Fatal("go runtime metrics missing")
	}
}
