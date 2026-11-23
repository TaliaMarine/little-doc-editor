package storage

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestFilesystemRoundTripAndTraversal(t *testing.T) {
	s, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Put(ctx, "a/b/c.txt", "text/plain", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(ctx, "a/b/c.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if b, _ := io.ReadAll(r); string(b) != "hello" {
		t.Fatalf("got %q", b)
	}
	if err := s.Put(ctx, "../escape", "", strings.NewReader("x"), 1); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}
