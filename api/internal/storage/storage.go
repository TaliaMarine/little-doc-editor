package storage

import (
	"context"
	"log/slog"
	"time"
)

// DeleteBestEffort removes blobs after their database rows are gone. Failures
// are logged, not returned: the rows (the source of truth) are already
// deleted, and an orphaned blob is a storage leak, not a correctness problem.
func DeleteBestEffort(ctx context.Context, s Storage, keys []string, logger *slog.Logger) {
	for _, k := range keys {
		if err := s.Delete(ctx, k); err != nil {
			logger.Warn("deleting attachment blob failed (orphaned object)", "key", k, "error", err)
		}
	}
}

// DeleteDetached is DeleteBestEffort in the background, on a context that
// outlives the HTTP request (a big delete would otherwise be cut short by the
// request timeout or a client disconnect, orphaning blobs).
func DeleteDetached(s Storage, keys []string, logger *slog.Logger) {
	if len(keys) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		DeleteBestEffort(ctx, s, keys, logger)
	}()
}
