package ratelimit

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestInProcessWindow(t *testing.T) {
	l := New(true, RedisOptions{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(ctx, "t", "a", 3, time.Minute); !ok {
			t.Fatalf("hit %d should pass", i)
		}
	}
	ok, retry := l.Allow(ctx, "t", "a", 3, time.Minute)
	if ok || retry <= 0 {
		t.Fatalf("4th hit must be limited with a retry-after, got ok=%v retry=%v", ok, retry)
	}
	if ok, _ := l.Allow(ctx, "t", "b", 3, time.Minute); !ok {
		t.Fatal("different subject has its own counter")
	}
	if ok, _ := l.Allow(ctx, "other", "a", 3, time.Minute); !ok {
		t.Fatal("different scope has its own counter")
	}
}

func TestDisabledAllowsEverything(t *testing.T) {
	l := New(false, RedisOptions{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < 100; i++ {
		if ok, _ := l.Allow(context.Background(), "t", "a", 1, time.Minute); !ok {
			t.Fatal("disabled limiter must not limit")
		}
	}
}

func TestWindowExpiry(t *testing.T) {
	l := New(true, RedisOptions{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	l.Allow(ctx, "t", "a", 1, 20*time.Millisecond)
	if ok, _ := l.Allow(ctx, "t", "a", 1, 20*time.Millisecond); ok {
		t.Fatal("should be limited")
	}
	time.Sleep(30 * time.Millisecond)
	if ok, _ := l.Allow(ctx, "t", "a", 1, 20*time.Millisecond); !ok {
		t.Fatal("window should have reset")
	}
}

func TestSubjectForIPGroupsIPv6By64(t *testing.T) {
	a := subjectForIP("2001:db8:1:2:aaaa:bbbb:cccc:dddd")
	b := subjectForIP("2001:db8:1:2:1111:2222:3333:4444")
	c := subjectForIP("2001:db8:1:3::1")
	if a != b || a == c {
		t.Fatalf("same /64 must share a key, different /64 must not: %s %s %s", a, b, c)
	}
	if subjectForIP("203.0.113.5") != "203.0.113.5" {
		t.Fatal("IPv4 keyed by address")
	}
}

func TestCountDoesNotIncrement(t *testing.T) {
	l := New(true, RedisOptions{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if n, _ := l.Count(ctx, "fail", "k"); n != 0 {
			t.Fatalf("Count must not increment, got %d", n)
		}
	}
	l.Allow(ctx, "fail", "k", 10, time.Minute)
	l.Allow(ctx, "fail", "k", 10, time.Minute)
	if n, ttl := l.Count(ctx, "fail", "k"); n != 2 || ttl <= 0 {
		t.Fatalf("expected count 2 with a ttl, got %d %v", n, ttl)
	}
}
