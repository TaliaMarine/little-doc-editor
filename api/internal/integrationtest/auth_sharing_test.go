//go:build integration

package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/oidcapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/passwordhash"
	"github.com/TaliaMarine/little-doc-editor/api/internal/server"
	"github.com/TaliaMarine/little-doc-editor/api/internal/sessiontoken"
	"github.com/TaliaMarine/little-doc-editor/api/internal/storage"
)

// newRealAuthTestServer is newTestServer's counterpart with real JWT auth
// instead of DEV_AUTH_BYPASS — needed for anything that tests role/identity
// boundaries, since bypass mode makes every request an org admin regardless
// of what token (if any) is sent. t.Setenv scopes the override to this test
// and restores the previous value afterward.
func newRealAuthTestServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, config.Config) {
	t.Helper()

	if _, ok := os.LookupEnv("DATABASE_URL"); !ok {
		t.Skip("DATABASE_URL not set — run via `make test-integration` / `just test-integration` after `docker compose up -d`")
	}
	t.Setenv("DEV_AUTH_BYPASS", "false")
	t.Setenv("SESSION_JWT_SECRET", "integration-test-secret")
	// Bootstrap-admin creation is a startup-time singleton ("only if no org
	// admin exists yet") that main.go handles — not applicable to a shared
	// long-lived test database, so tests seed their own admin directly via
	// seedOrgAdmin instead of relying on it.
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Postgres not reachable at %s: %v — is `docker compose up -d` running?", cfg.DatabaseURL, err)
	}

	store, err := storage.New(ctx, cfg)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	oidcHandler, err := oidcapi.NewHandler(ctx, db.New(pool), cfg, logger)
	if err != nil {
		t.Fatalf("oidcapi.NewHandler: %v", err)
	}

	ts := httptest.NewServer(server.New(logger, pool, store, oidcHandler, cfg))
	t.Cleanup(ts.Close)
	return ts, pool, cfg
}

// seedOrgAdmin creates a real org-admin user directly (bypassing the
// bootstrap singleton's "only if none exists" check, and the login
// endpoint, both already covered elsewhere — see TestLogin) and returns a
// ready-to-use session token for them.
func seedOrgAdmin(t *testing.T, pool *pgxpool.Pool, cfg config.Config, displayName string) (uuid.UUID, string) {
	t.Helper()
	hash, err := passwordhash.Hash("irrelevant-not-used-for-login-in-this-test")
	if err != nil {
		t.Fatalf("passwordhash.Hash: %v", err)
	}
	user, err := db.New(pool).CreateInternalOrgAdmin(context.Background(), db.CreateInternalOrgAdminParams{
		Email:        "admin-" + uuid.NewString() + "@example.com",
		DisplayName:  displayName,
		PasswordHash: &hash,
	})
	if err != nil {
		t.Fatalf("seeding org admin: %v", err)
	}
	userID := db.FromPgUUID(user.ID)
	token, err := sessiontoken.IssueForUser(cfg.SessionJWTSecret, userID, displayName, time.Hour)
	if err != nil {
		t.Fatalf("issuing admin token: %v", err)
	}
	return userID, token
}

// seedOutsider creates a real internal user with no space memberships at
// all, and returns a valid session token for them — used to prove role
// checks actually reject someone who is authenticated but not authorized,
// as opposed to just testing the "no token at all" case.
func seedOutsider(t *testing.T, pool *pgxpool.Pool, cfg config.Config, displayName string) (uuid.UUID, string) {
	t.Helper()
	hash, err := passwordhash.Hash("irrelevant-not-used-for-login-in-this-test")
	if err != nil {
		t.Fatalf("passwordhash.Hash: %v", err)
	}
	user, err := db.New(pool).CreateInternalUser(context.Background(), db.CreateInternalUserParams{
		Email:        "outsider-" + uuid.NewString() + "@example.com",
		DisplayName:  displayName,
		PasswordHash: &hash,
	})
	if err != nil {
		t.Fatalf("seeding outsider user: %v", err)
	}
	userID := db.FromPgUUID(user.ID)
	token, err := sessiontoken.IssueForUser(cfg.SessionJWTSecret, userID, displayName, time.Hour)
	if err != nil {
		t.Fatalf("issuing outsider token: %v", err)
	}
	return userID, token
}

func TestLogin(t *testing.T) {
	ts, pool, _ := newRealAuthTestServer(t)
	client := ts.Client()

	email := "login-test-" + uuid.NewString() + "@example.com"
	hash, err := passwordhash.Hash("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("passwordhash.Hash: %v", err)
	}
	user, err := db.New(pool).CreateInternalUser(context.Background(), db.CreateInternalUserParams{
		Email:        email,
		DisplayName:  "Login Test User",
		PasswordHash: &hash,
	})
	if err != nil {
		t.Fatalf("seeding login user: %v", err)
	}

	t.Run("correct credentials succeed and the token works", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"email": email, "password": "correct-horse-battery-staple"})
		resp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login request: %v", err)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
		}
		var loginResp struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(respBody, &loginResp); err != nil {
			t.Fatalf("decoding login response: %v", err)
		}

		status, _ := authedGet(t, client, ts.URL+"/api/v1/auth/me", loginResp.Token)
		if status != http.StatusOK {
			t.Errorf("expected the issued token to work against /auth/me, got %d", status)
		}
	})

	t.Run("wrong password is rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"email": email, "password": "wrong"})
		resp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("unknown email is rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"email": "nobody-" + uuid.NewString() + "@example.com", "password": "whatever"})
		resp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("disabled account is rejected even with the correct password", func(t *testing.T) {
		if err := db.New(pool).SetUserDisabled(context.Background(), db.SetUserDisabledParams{
			ID:       user.ID,
			Disabled: true,
		}); err != nil {
			t.Fatalf("disabling user: %v", err)
		}
		body, _ := json.Marshal(map[string]string{"email": email, "password": "correct-horse-battery-staple"})
		resp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 for a disabled account, got %d", resp.StatusCode)
		}
	})
}

func TestRoleEnforcement(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Role Enforcement Space", uniqueSlug(t))
	pageID := createPageAs(t, client, ts.URL, adminToken, spaceID)
	_, outsiderToken := seedOutsider(t, pool, cfg, "Outsider")

	t.Run("authenticated non-member is rejected from a page in a space they're not in", func(t *testing.T) {
		status, _ := authedGet(t, client, ts.URL+"/api/v1/pages/"+pageID, outsiderToken)
		if status != http.StatusForbidden {
			t.Errorf("expected 403, got %d", status)
		}
	})

	t.Run("authenticated non-member cannot create a page", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"title": "x", "content": map[string]any{}})
		status, _ := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/pages", outsiderToken, body)
		if status != http.StatusForbidden {
			t.Errorf("expected 403, got %d", status)
		}
	})

	t.Run("authenticated non-org-admin cannot create a space", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "Nope", "slug": uniqueSlug(t)})
		status, _ := authedPost(t, client, ts.URL+"/api/v1/spaces", outsiderToken, body)
		if status != http.StatusForbidden {
			t.Errorf("expected 403, got %d", status)
		}
	})

	t.Run("promoting to viewer grants read but not write", func(t *testing.T) {
		outsiderID, token := seedOutsider(t, pool, cfg, "Promoted Viewer")
		addMemberBody, _ := json.Marshal(map[string]string{"userId": outsiderID.String(), "role": "viewer"})
		status, respBody := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/members", adminToken, addMemberBody)
		if status != http.StatusCreated {
			t.Fatalf("adding member: expected 201, got %d: %s", status, respBody)
		}

		status, _ = authedGet(t, client, ts.URL+"/api/v1/pages/"+pageID, token)
		if status != http.StatusOK {
			t.Errorf("viewer should be able to read the page, got %d", status)
		}

		updateBody, _ := json.Marshal(map[string]any{"title": "hacked", "content": map[string]any{}})
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/pages/"+pageID+"/content", bytes.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("update request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("viewer should NOT be able to write, got %d", resp.StatusCode)
		}
	})

	t.Run("disabled user is rejected even with a still-valid token", func(t *testing.T) {
		outsiderID, token := seedOutsider(t, pool, cfg, "Soon Disabled")
		if err := db.New(pool).SetUserDisabled(context.Background(), db.SetUserDisabledParams{
			ID:       db.ToPgUUID(outsiderID),
			Disabled: true,
		}); err != nil {
			t.Fatalf("disabling user: %v", err)
		}
		status, _ := authedGet(t, client, ts.URL+"/api/v1/auth/me", token)
		if status != http.StatusUnauthorized {
			t.Errorf("expected 401 for a disabled user, got %d", status)
		}
	})
}

func TestShareLinkFlow(t *testing.T) {
	ts, pool, cfg := newRealAuthTestServer(t)
	client := ts.Client()

	_, adminToken := seedOrgAdmin(t, pool, cfg, "Admin")
	spaceID := createSpaceAs(t, client, ts.URL, adminToken, "Share Flow Space", uniqueSlug(t))
	pageID := createPageAs(t, client, ts.URL, adminToken, spaceID)

	childBody, _ := json.Marshal(map[string]any{"title": "Child", "content": map[string]any{}, "parentId": pageID})
	status, respBody := authedPost(t, client, ts.URL+"/api/v1/spaces/"+spaceID+"/pages", adminToken, childBody)
	if status != http.StatusCreated {
		t.Fatalf("creating child page: expected 201, got %d: %s", status, respBody)
	}
	var child struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &child); err != nil {
		t.Fatalf("decoding child page: %v", err)
	}

	t.Run("view link grants descendant access but not edit", func(t *testing.T) {
		linkBody, _ := json.Marshal(map[string]string{"permission": "view"})
		status, respBody := authedPost(t, client, ts.URL+"/api/v1/pages/"+pageID+"/share-links", adminToken, linkBody)
		if status != http.StatusCreated {
			t.Fatalf("creating share link: expected 201, got %d: %s", status, respBody)
		}
		var link struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(respBody, &link); err != nil {
			t.Fatalf("decoding share link: %v", err)
		}

		redeemBody, _ := json.Marshal(map[string]string{"displayName": "Anonymous Otter"})
		resp, err := client.Post(ts.URL+"/api/v1/share/"+link.Token+"/redeem", "application/json", bytes.NewReader(redeemBody))
		if err != nil {
			t.Fatalf("redeem request: %v", err)
		}
		defer resp.Body.Close()
		redeemRespBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("redeem: expected 200, got %d: %s", resp.StatusCode, redeemRespBody)
		}
		var redeemed struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(redeemRespBody, &redeemed); err != nil {
			t.Fatalf("decoding redeem response: %v", err)
		}

		if status, _ := authedGet(t, client, ts.URL+"/api/v1/pages/"+pageID, redeemed.Token); status != http.StatusOK {
			t.Errorf("guest should be able to view the shared page, got %d", status)
		}
		if status, _ := authedGet(t, client, ts.URL+"/api/v1/pages/"+child.ID, redeemed.Token); status != http.StatusOK {
			t.Errorf("guest should be able to view the shared page's descendant, got %d", status)
		}

		updateBody, _ := json.Marshal(map[string]any{"title": "hacked", "content": map[string]any{}})
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/pages/"+pageID+"/content", bytes.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer "+redeemed.Token)
		resp2, err := client.Do(req)
		if err != nil {
			t.Fatalf("update request: %v", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden {
			t.Errorf("view-only guest should NOT be able to edit, got %d", resp2.StatusCode)
		}
	})

	t.Run("password-protected link rejects wrong password and accepts the right one", func(t *testing.T) {
		linkBody, _ := json.Marshal(map[string]string{"permission": "edit", "password": "letmein"})
		status, respBody := authedPost(t, client, ts.URL+"/api/v1/pages/"+pageID+"/share-links", adminToken, linkBody)
		if status != http.StatusCreated {
			t.Fatalf("creating share link: expected 201, got %d: %s", status, respBody)
		}
		var link struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(respBody, &link); err != nil {
			t.Fatalf("decoding share link: %v", err)
		}

		wrongBody, _ := json.Marshal(map[string]string{"password": "wrong", "displayName": "X"})
		resp, err := client.Post(ts.URL+"/api/v1/share/"+link.Token+"/redeem", "application/json", bytes.NewReader(wrongBody))
		if err != nil {
			t.Fatalf("redeem request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 for wrong password, got %d", resp.StatusCode)
		}

		rightBody, _ := json.Marshal(map[string]string{"password": "letmein", "displayName": "Editor Otter"})
		resp2, err := client.Post(ts.URL+"/api/v1/share/"+link.Token+"/redeem", "application/json", bytes.NewReader(rightBody))
		if err != nil {
			t.Fatalf("redeem request: %v", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp2.Body)
			t.Fatalf("expected 200 for correct password, got %d: %s", resp2.StatusCode, body)
		}
	})

	t.Run("revoked link can no longer be resolved or redeemed", func(t *testing.T) {
		linkBody, _ := json.Marshal(map[string]string{"permission": "view"})
		status, respBody := authedPost(t, client, ts.URL+"/api/v1/pages/"+pageID+"/share-links", adminToken, linkBody)
		if status != http.StatusCreated {
			t.Fatalf("creating share link: expected 201, got %d: %s", status, respBody)
		}
		var link struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal(respBody, &link); err != nil {
			t.Fatalf("decoding share link: %v", err)
		}

		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/share-links/"+link.ID, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("revoke request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("expected 204 revoking link, got %d", resp.StatusCode)
		}

		resp2, err := client.Get(ts.URL + "/api/v1/share/" + link.Token)
		if err != nil {
			t.Fatalf("resolve request: %v", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusGone {
			t.Errorf("expected 410 for a revoked link, got %d", resp2.StatusCode)
		}
	})
}

func authedGet(t *testing.T, client *http.Client, url, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func authedPost(t *testing.T, client *http.Client, url, token string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody
}

// devAdminToken is used by attachment_flow_test.go's bypass-mode helpers
// (createSpace/createPage) — the dev-bypass middleware ignores the token's
// actual content, so any non-empty bearer value works there.
func devAdminToken(t *testing.T) string {
	t.Helper()
	return "dev-admin-bypass"
}
