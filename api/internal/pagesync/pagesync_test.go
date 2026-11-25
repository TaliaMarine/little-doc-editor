package pagesync

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestNilNotifierIsNoop(t *testing.T) {
	var n *Notifier
	if err := n.PageContentChanged(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	n.Close()
	if New(Options{}) != nil {
		t.Fatal("empty addr must give the nil no-op notifier")
	}
}

func TestUnreachableRedisReturnsError(t *testing.T) {
	n := New(Options{Addr: "127.0.0.1:1"})
	defer n.Close()
	if err := n.PageContentChanged(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected an error so callers can log it")
	}
}

func TestPublishesPageID(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set")
	}
	sub := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD")})
	defer sub.Close()
	ctx := context.Background()
	ps := sub.Subscribe(ctx, Channel)
	defer ps.Close()
	if _, err := ps.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	n := New(Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD")})
	defer n.Close()
	id := uuid.New()
	if err := n.PageContentChanged(ctx, id); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-ps.Channel():
		if m.Payload != id.String() {
			t.Fatalf("payload %q, want %q", m.Payload, id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no message received")
	}
}
