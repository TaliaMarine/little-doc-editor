// Package pagesync tells the collab sidecar that pages.content was changed
// outside of collab (revision restore, PUT /pages/{id}/content), so a live
// room for that page can merge the new content immediately instead of at its
// next store (collab/src/contentSync.ts is the subscriber).
//
// The message is best-effort: it is published after the database commit, a
// failure is logged by the caller and never fails the request, and a missed
// message only means collab falls back to its store-time detection.
package pagesync

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Channel is the Redis pub/sub channel; the payload is the page id (a UUID
// string) and nothing else. Keep in sync with collab/src/contentSync.ts.
const Channel = "lde:page-content-replaced"

// Options mirror the REDIS_* settings. Addr empty disables publishing.
type Options struct {
	Addr, Username, Password string
	TLS                      bool
}

// Notifier publishes page-content-changed messages. The zero/nil value is a
// no-op, so callers need no nil checks.
type Notifier struct {
	rdb *redis.Client
}

// New returns a Notifier, or nil (a valid no-op) when Addr is empty.
func New(o Options) *Notifier {
	if o.Addr == "" {
		return nil
	}
	opts := &redis.Options{
		Addr:         o.Addr,
		Username:     o.Username,
		Password:     o.Password,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		MaxRetries:   0,
	}
	if o.TLS {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return &Notifier{rdb: redis.NewClient(opts)}
}

// PageContentChanged publishes the page id. It is detached from ctx
// cancellation (the response may already be on its way) and bounded by a
// short timeout so a Redis outage cannot stall the request for long.
func (n *Notifier) PageContentChanged(ctx context.Context, pageID uuid.UUID) error {
	if n == nil || n.rdb == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return n.rdb.Publish(ctx, Channel, pageID.String()).Err()
}

// Close releases the Redis connection pool.
func (n *Notifier) Close() {
	if n != nil && n.rdb != nil {
		_ = n.rdb.Close()
	}
}
