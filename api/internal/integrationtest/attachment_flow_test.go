//go:build integration

// Package integrationtest exercises the api service against real
// dependencies (Postgres, S3-compatible storage) rather than mocks — a
// mocked S3 client would not have caught either of the two real bugs this
// suite is built around (jsonb content serializing as base64, and the AWS
// SDK's checksum handling needing a seekable request body). Run via
// `make test-integration` / `just test-integration`, which bring up
// docker-compose first. See DEVELOPER.md for the full local setup.
package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/oidcapi"
	"github.com/TaliaMarine/little-doc-editor/api/internal/server"
	"github.com/TaliaMarine/little-doc-editor/api/internal/storage"
)

// pngBytes is a minimal valid 1x1 PNG — real magic bytes, not a stub, so the
// api's content-type sniffing has something genuine to detect.
var pngBytes = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xDE,
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	if _, ok := os.LookupEnv("DATABASE_URL"); !ok {
		t.Skip("DATABASE_URL not set — run via `make test-integration` / `just test-integration` after `docker compose up -d`")
	}

	t.Setenv("RATE_LIMIT_ENABLED", "false") // the limiter has its own test
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v (integration tests need DEV_AUTH_BYPASS=true and S3_* set — see DEVELOPER.md)", err)
	}
	if !cfg.DevAuthBypass {
		t.Fatal("DEV_AUTH_BYPASS must be true to run this suite — every request would otherwise get 401")
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

	// cmd/api/main.go seeds this row on every real startup when
	// DEV_AUTH_BYPASS is set; this test harness builds the server directly
	// via server.New rather than running main(), so it has to do the same
	// seeding itself, or every FK-bearing insert attributed to the bypass
	// identity (pages, attachments, ...) fails.
	devAdminUserID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	passwordHash := "dev-bypass-not-a-real-hash"
	if _, err := db.New(pool).UpsertDevAdminUser(ctx, db.UpsertDevAdminUserParams{
		ID:           db.ToPgUUID(devAdminUserID),
		Email:        "dev-admin@localhost",
		DisplayName:  "Local Dev Admin",
		PasswordHash: &passwordHash,
	}); err != nil {
		t.Fatalf("seeding dev admin user: %v", err)
	}

	store, err := storage.New(ctx, cfg)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	oidcHandler, err := oidcapi.NewHandler(ctx, db.New(pool), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("oidcapi.NewHandler: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(server.New(logger, pool, store, oidcHandler, cfg))
	t.Cleanup(ts.Close)
	return ts
}

func TestAttachmentUploadFlow(t *testing.T) {
	ts := newTestServer(t)
	client := ts.Client()

	spaceID := createSpace(t, client, ts.URL, "Integration Test Space", uniqueSlug(t))
	pageID := createPage(t, client, ts.URL, spaceID)

	t.Run("valid PNG upload round-trips byte-for-byte", func(t *testing.T) {
		status, body := uploadAttachment(t, client, ts.URL, pageID, "test.png", "application/octet-stream", pngBytes)
		if status != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", status, body)
		}

		var attachment struct {
			ID          string `json:"id"`
			ContentType string `json:"content_type"`
			SizeBytes   int64  `json:"size_bytes"`
		}
		if err := json.Unmarshal(body, &attachment); err != nil {
			t.Fatalf("decoding response: %v (body: %s)", err, body)
		}

		// The client claimed application/octet-stream above — the server
		// must have sniffed the real type from content, not trusted that.
		if attachment.ContentType != "image/png" {
			t.Errorf("expected sniffed content type image/png, got %q", attachment.ContentType)
		}
		if attachment.SizeBytes != int64(len(pngBytes)) {
			t.Errorf("expected size %d, got %d", len(pngBytes), attachment.SizeBytes)
		}

		resp, err := client.Get(ts.URL + "/api/v1/attachments/" + attachment.ID)
		if err != nil {
			t.Fatalf("download request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 downloading attachment, got %d", resp.StatusCode)
		}
		downloaded, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("reading download body: %v", err)
		}
		if !bytes.Equal(downloaded, pngBytes) {
			t.Errorf("downloaded bytes don't match uploaded bytes: got %d bytes, want %d", len(downloaded), len(pngBytes))
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
			t.Errorf("expected Content-Type image/png on download, got %q", ct)
		}
	})

	t.Run("disallowed type is rejected before reaching storage", func(t *testing.T) {
		elfBytes := append([]byte{0x7F, 'E', 'L', 'F', 2, 1, 1, 0}, bytes.Repeat([]byte{0}, 32)...)
		status, body := uploadAttachment(t, client, ts.URL, pageID, "evil.png", "image/png", elfBytes)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for disallowed type, got %d: %s", status, body)
		}
	})

	t.Run("oversized upload is rejected", func(t *testing.T) {
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		oversized := append(append([]byte{}, pngBytes...), make([]byte, cfg.AttachmentMaxSizeBytes)...)
		status, body := uploadAttachment(t, client, ts.URL, pageID, "huge.png", "image/png", oversized)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for oversized upload, got %d: %s", status, body)
		}
	})

	t.Run("upload to a nonexistent page 404s", func(t *testing.T) {
		status, body := uploadAttachment(t, client, ts.URL, "00000000-0000-0000-0000-000000000099", "test.png", "image/png", pngBytes)
		if status != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", status, body)
		}
	})
}

func TestUnauthenticatedRequestsAreRejectedWithoutBypass(t *testing.T) {
	// This suite runs with DEV_AUTH_BYPASS=true throughout (required by
	// newTestServer), so it can't exercise the 401 path against the same
	// server. This test documents that gap explicitly rather than silently
	// having no coverage for it — the 401 branch in authctx.Middleware is
	// straight-line code with no dependencies, so it's covered by ordinary
	// (non-integration) unit tests instead. See internal/authctx.
	t.Skip("covered by unit tests in internal/authctx; needs DEV_AUTH_BYPASS=false, which this suite can't flip mid-run")
}

// createSpace/createPage always send a bearer token. Under a dev-bypass
// server the token's content is ignored (any non-empty value works, see
// devAdminToken); under a real-auth server it must be a valid admin token.
// Sending it unconditionally lets both test files share these helpers.
func createSpace(t *testing.T, client *http.Client, baseURL, name, slug string) string {
	return createSpaceAs(t, client, baseURL, devAdminToken(t), name, slug)
}

func createSpaceAs(t *testing.T, client *http.Client, baseURL, token, name, slug string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name, "slug": slug})
	status, respBody := authedPost(t, client, baseURL+"/api/v1/spaces", token, body)
	if status != http.StatusCreated {
		t.Fatalf("create space: expected 201, got %d: %s", status, respBody)
	}
	var space struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &space); err != nil {
		t.Fatalf("decoding space response: %v", err)
	}
	return space.ID
}

func createPage(t *testing.T, client *http.Client, baseURL, spaceID string) string {
	return createPageAs(t, client, baseURL, devAdminToken(t), spaceID)
}

func createPageAs(t *testing.T, client *http.Client, baseURL, token, spaceID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"title":   "Integration Test Page",
		"content": map[string]any{"type": "doc", "content": []any{}},
	})
	status, respBody := authedPost(t, client, baseURL+"/api/v1/spaces/"+spaceID+"/pages", token, body)
	if status != http.StatusCreated {
		t.Fatalf("create page: expected 201, got %d: %s", status, respBody)
	}
	var page struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &page); err != nil {
		t.Fatalf("decoding page response: %v", err)
	}
	return page.ID
}

func uploadAttachment(t *testing.T, client *http.Client, baseURL, pageID, filename, claimedContentType string, data []byte) (int, []byte) {
	t.Helper()
	return uploadAttachmentAs(t, client, baseURL, devAdminToken(t), pageID, filename, claimedContentType, data)
}

// uploadAttachmentAs is uploadAttachment with a real bearer token, for the
// real-auth test server.
func uploadAttachmentAs(t *testing.T, client *http.Client, baseURL, token, pageID, filename, claimedContentType string, data []byte) (int, []byte) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreatePart(map[string][]string{
		// Escaped like a browser would, so filenames containing quotes etc.
		// still produce a well-formed part.
		"Content-Disposition": {mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename})},
		"Content-Type":        {claimedContentType},
	})
	if err != nil {
		t.Fatalf("creating multipart part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("writing multipart body: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/pages/"+pageID+"/attachments", &buf)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading upload response: %v", err)
	}
	return resp.StatusCode, respBody
}

func uniqueSlug(t *testing.T) string {
	t.Helper()
	return "integration-" + strings.ToLower(t.Name()) + "-" + time.Now().UTC().Format("150405.000000000")
}
