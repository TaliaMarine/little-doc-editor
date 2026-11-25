// Package ratelimit is the fixed-window rate limiter that protects the
// unauthenticated, hash- or token-checking endpoints (SPEC.md §8): login,
// share-link redemption and invite redemption, each of which costs an
// argon2id hash or writes rows.
//
// Decision (recorded in SPEC.md): counters live in Redis so the limit holds
// across api replicas (Decision 3). If Redis is unconfigured or unreachable
// the limiter degrades to an in-process counter — per-replica, but never
// fail-open and never a login outage because Redis blipped.
package ratelimit

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/TaliaMarine/little-doc-editor/api/internal/clientip"
	"github.com/TaliaMarine/little-doc-editor/api/internal/httpx"
)

// incrScript increments the window counter, starting the TTL on first hit,
// and returns {count, ttl_ms}.
var incrScript = redis.NewScript(`
local c = redis.call('INCR', KEYS[1])
if c == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return {c, redis.call('PTTL', KEYS[1])}
`)

type Limiter struct {
	enabled bool
	rdb     *redis.Client
	mem     *memStore
	logger  *slog.Logger
	ips     *clientip.Resolver

	warnMu   sync.Mutex
	lastWarn time.Time
}

// RedisOptions: Addr empty means in-process counters only.
type RedisOptions struct {
	Addr, Username, Password string
	TLS                      bool
	DB                       int
}

// New builds a limiter.
func New(enabled bool, ro RedisOptions, ips *clientip.Resolver, logger *slog.Logger) *Limiter {
	l := &Limiter{enabled: enabled, mem: newMemStore(), logger: logger, ips: ips}
	if enabled && ro.Addr != "" {
		opts := &redis.Options{
			Addr:         ro.Addr,
			Username:     ro.Username,
			Password:     ro.Password,
			DB:           ro.DB,
			DialTimeout:  time.Second,
			ReadTimeout:  500 * time.Millisecond,
			WriteTimeout: 500 * time.Millisecond,
			MaxRetries:   0,
		}
		if ro.TLS {
			opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		l.rdb = redis.NewClient(opts)
	}
	return l
}

func (l *Limiter) Close() {
	if l.rdb != nil {
		_ = l.rdb.Close()
	}
}

// Allow counts one hit against (scope, subject) and reports whether it is
// within limit per window. When over, retryAfter says when the window ends.
// subject is hashed before use so raw IPs/emails never land in Redis keys.
func (l *Limiter) Allow(ctx context.Context, scope, subject string, limit int, window time.Duration) (bool, time.Duration) {
	if l == nil || !l.enabled {
		return true, 0
	}
	key := l.key(scope, subject)

	if l.rdb != nil {
		res, err := incrScript.Run(ctx, l.rdb, []string{key}, window.Milliseconds()).Int64Slice()
		if err == nil && len(res) == 2 {
			if res[0] <= int64(limit) {
				return true, 0
			}
			ttl := time.Duration(res[1]) * time.Millisecond
			if ttl <= 0 {
				ttl = window
			}
			return false, ttl
		}
		l.warnRedis(err)
	}
	return l.mem.allow(key, limit, window)
}

func (l *Limiter) warnRedis(err error) {
	l.warnMu.Lock()
	defer l.warnMu.Unlock()
	if time.Since(l.lastWarn) > time.Minute {
		l.lastWarn = time.Now()
		l.logger.Warn("rate limiter: redis unavailable, using per-replica in-process counters", "error", err)
	}
}

// Count reads the current counter for (scope, subject) without incrementing
// it, so callers can count only failures (Allow after a failed attempt) while
// checking before every attempt. Returns the count and the time left in the
// window.
func (l *Limiter) Count(ctx context.Context, scope, subject string) (int, time.Duration) {
	if l == nil || !l.enabled {
		return 0, 0
	}
	key := l.key(scope, subject)
	if l.rdb != nil {
		pipe := l.rdb.Pipeline()
		get := pipe.Get(ctx, key)
		ttl := pipe.PTTL(ctx, key)
		if _, err := pipe.Exec(ctx); err == nil || errors.Is(err, redis.Nil) {
			n, _ := get.Int()
			return n, ttl.Val()
		} else {
			l.warnRedis(err)
		}
	}
	return l.mem.count(key)
}

// ClientKey is the rate-limit subject for the request's client (IPv6 by /64).
func (l *Limiter) ClientKey(r *http.Request) string {
	if l == nil {
		return ""
	}
	return subjectForIP(l.ips.IP(r))
}

func (l *Limiter) key(scope, subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return "lde:rl:" + scope + ":" + hex.EncodeToString(sum[:16])
}

// Middleware limits requests per client IP under scope.
func (l *Limiter) Middleware(scope string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, retry := l.Allow(r.Context(), scope, subjectForIP(l.ips.IP(r)), limit, window); !ok {
				Reject(w, retry)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// subjectForIP keys IPv4 clients by address and IPv6 clients by their /64:
// a single subscriber typically controls a whole /64, so per-address keys
// would give an attacker 2^64 fresh buckets.
func subjectForIP(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || a.Is4() || a.Is4In6() {
		return ip
	}
	p, err := a.Prefix(64)
	if err != nil {
		return ip
	}
	return p.String()
}

// Reject writes the 429 response.
func Reject(w http.ResponseWriter, retryAfter time.Duration) {
	secs := int(retryAfter.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	httpx.WriteError(w, http.StatusTooManyRequests, "too many requests, try again later")
}

type memEntry struct {
	count int
	reset time.Time
}

const (
	memSweepEvery = time.Minute
	memMaxEntries = 100000
)

type memStore struct {
	mu        sync.Mutex
	m         map[string]*memEntry
	lastSweep time.Time
}

func newMemStore() *memStore { return &memStore{m: map[string]*memEntry{}} }

func (s *memStore) count(key string) (int, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok || time.Now().After(e.reset) {
		return 0, 0
	}
	return e.count, time.Until(e.reset)
}

func (s *memStore) allow(key string, limit int, window time.Duration) (bool, time.Duration) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	// Bound memory without an O(n) scan per request: sweep expired windows at
	// most once a minute, and if an attack still overfills the map evict
	// arbitrary entries (map order is random) rather than grow or fail closed.
	if len(s.m) > memMaxEntries || now.Sub(s.lastSweep) > memSweepEvery {
		s.lastSweep = now
		for k, e := range s.m {
			if now.After(e.reset) {
				delete(s.m, k)
			}
		}
		for k := range s.m {
			if len(s.m) <= memMaxEntries/2 {
				break
			}
			delete(s.m, k)
		}
	}
	e, ok := s.m[key]
	if !ok || now.After(e.reset) {
		e = &memEntry{reset: now.Add(window)}
		s.m[key] = e
	}
	e.count++
	if e.count <= limit {
		return true, 0
	}
	return false, time.Until(e.reset)
}
