//go:build integration

package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
)

// env is a real-auth test server plus tiny request helpers, for the feature
// tests below (accounts, history, export, hardening...).
type env struct {
	t      *testing.T
	ts     *httptest.Server
	pool   *pgxpool.Pool
	cfg    config.Config
	q      *db.Queries
	admin  uuid.UUID
	adminT string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ts, pool, cfg := newRealAuthTestServer(t)
	e := &env{t: t, ts: ts, pool: pool, cfg: cfg, q: db.New(pool)}
	e.admin, e.adminT = seedOrgAdmin(t, pool, cfg, "Feature Admin")
	return e
}

// do sends a JSON request (body may be nil, []byte, or any JSON-marshalable
// value) and returns status + raw body. token may be "" for anonymous.
func (e *env) do(method, path, token string, body any) (int, []byte) {
	e.t.Helper()
	var rdr io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rdr = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.ts.URL+"/api/v1"+path, rdr)
	if err != nil {
		e.t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if rdr != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.ts.Client().Do(req)
	if err != nil {
		e.t.Fatalf("request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (e *env) expect(want int, method, path, token string, body any) []byte {
	e.t.Helper()
	status, out := e.do(method, path, token, body)
	if status != want {
		e.t.Fatalf("%s %s: expected %d, got %d: %s", method, path, want, status, out)
	}
	return out
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return v
}

type idOnly struct {
	ID string `json:"id"`
}

func (e *env) newUser(name string) (uuid.UUID, string) {
	e.t.Helper()
	return seedOutsider(e.t, e.pool, e.cfg, name)
}

func (e *env) newSpace() string {
	e.t.Helper()
	return createSpaceAs(e.t, e.ts.Client(), e.ts.URL, e.adminT, "Feature Space", uniqueSlug(e.t))
}

func (e *env) addMember(spaceID string, userID uuid.UUID, role string) {
	e.t.Helper()
	e.expect(http.StatusCreated, "POST", "/spaces/"+spaceID+"/members", e.adminT, map[string]string{"userId": userID.String(), "role": role})
}

func (e *env) newPage(token, spaceID, title string, content any, parent string) string {
	e.t.Helper()
	body := map[string]any{"title": title, "content": content}
	if parent != "" {
		body["parentId"] = parent
	}
	return decode[idOnly](e.t, e.expect(http.StatusCreated, "POST", "/spaces/"+spaceID+"/pages", token, body)).ID
}

func docWith(text string) map[string]any {
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}},
	}}
}

func (e *env) save(token, pageID, title string, content any) (int, []byte) {
	e.t.Helper()
	return e.do("PUT", "/pages/"+pageID+"/content", token, map[string]any{"title": title, "content": content})
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func jsonEnc(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) }
