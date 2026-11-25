//go:build integration

package oidcapi

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
)

func TestSyncGroupsMirrorsTheIdPClaim(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := db.New(pool)
	h := &Handler{queries: q}

	user, err := q.CreateOIDCUser(ctx, db.CreateOIDCUserParams{
		Email: "g-" + uuid.NewString() + "@example.com", DisplayName: "G",
		OidcIssuer: strp("https://idp"), OidcSubject: strp(uuid.NewString()),
	})
	if err != nil {
		t.Fatal(err)
	}
	names := func() []string {
		rows, err := pool.Query(ctx, `SELECT g.name FROM group_members gm JOIN groups g ON g.id = gm.group_id WHERE gm.user_id = $1`, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			out = append(out, n)
		}
		sort.Strings(out)
		return out
	}
	a, b, c := "ga-"+uuid.NewString(), "gb-"+uuid.NewString(), "gc-"+uuid.NewString()

	if err := h.syncGroups(ctx, user.ID, []string{a, b, " "}); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 2 {
		t.Fatalf("expected 2 memberships, got %v", got)
	}
	// IdP drops a, adds c: a is removed from the user (group row remains), c appears
	if err := h.syncGroups(ctx, user.ID, []string{b, c}); err != nil {
		t.Fatal(err)
	}
	got := names()
	want := []string{b, c}
	sort.Strings(want)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v want %v", got, want)
	}
	if err := h.syncGroups(ctx, user.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 0 {
		t.Fatalf("empty claim must clear memberships, got %v", got)
	}
}

func strp(s string) *string { return &s }
