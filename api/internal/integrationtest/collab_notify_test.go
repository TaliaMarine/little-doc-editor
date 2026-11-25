//go:build integration

package integrationtest

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
	"github.com/TaliaMarine/little-doc-editor/api/internal/pagesync"
)

func newEnvWith(t *testing.T, extra map[string]string) *env {
	t.Helper()
	ts, pool, cfg := newRealAuthTestServerWith(t, extra)
	e := &env{t: t, ts: ts, pool: pool, cfg: cfg, q: db.New(pool)}
	e.admin, e.adminT = seedOrgAdmin(t, pool, cfg, "Notify Admin")
	return e
}

// Restore and PUT /content publish the page id on pagesync.Channel so collab
// can merge the change into a live room; an unreachable Redis must not fail
// either request.
func TestContentChangesNotifyCollab(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set")
	}
	e := newEnvWith(t, nil)
	spaceID := e.newSpace()
	pageID := e.newPage(e.adminT, spaceID, "Notify", docWith("version one"), "")
	e.expect(http.StatusOK, "PUT", "/pages/"+pageID+"/content", e.adminT, map[string]any{"title": "Notify", "content": docWith("version two")})

	ctx := context.Background()
	sub := redis.NewClient(&redis.Options{Addr: addr, Username: os.Getenv("REDIS_USERNAME"), Password: os.Getenv("REDIS_PASSWORD")})
	defer sub.Close()
	ps := sub.Subscribe(ctx, pagesync.Channel)
	defer ps.Close()
	if _, err := ps.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	wait := func(what string) {
		t.Helper()
		select {
		case m := <-ps.Channel():
			if m.Payload != pageID {
				t.Fatalf("%s: payload %q, want page id %q", what, m.Payload, pageID)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: no notification published", what)
		}
	}

	e.expect(http.StatusOK, "PUT", "/pages/"+pageID+"/content", e.adminT, map[string]any{"title": "Notify", "content": docWith("version three")})
	wait("PUT /content")

	// a title-only save does not change content: no message
	e.expect(http.StatusOK, "PUT", "/pages/"+pageID+"/content", e.adminT, map[string]any{"title": "Renamed"})
	select {
	case m := <-ps.Channel():
		t.Fatalf("title-only save must not notify, got %q", m.Payload)
	case <-time.After(300 * time.Millisecond):
	}

	revs := decode[[]revRow](t, e.expect(http.StatusOK, "GET", "/pages/"+pageID+"/revisions", e.adminT, nil))
	if len(revs) == 0 {
		t.Fatal("expected revisions")
	}
	e.expect(http.StatusOK, "POST", "/pages/"+pageID+"/revisions/"+revs[len(revs)-1].ID+"/restore", e.adminT, nil)
	wait("restore")
}

func TestRestoreSucceedsWhenRedisIsDown(t *testing.T) {
	e := newEnvWith(t, map[string]string{"REDIS_ADDR": "127.0.0.1:1"})
	spaceID := e.newSpace()
	pageID := e.newPage(e.adminT, spaceID, "Down", docWith("version one"), "")
	e.expect(http.StatusOK, "PUT", "/pages/"+pageID+"/content", e.adminT, map[string]any{"title": "Down", "content": docWith("version two")})
	revs := decode[[]revRow](t, e.expect(http.StatusOK, "GET", "/pages/"+pageID+"/revisions", e.adminT, nil))
	e.expect(http.StatusOK, "POST", "/pages/"+pageID+"/revisions/"+revs[len(revs)-1].ID+"/restore", e.adminT, nil)
}
