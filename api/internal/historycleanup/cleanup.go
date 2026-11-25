// Package historycleanup is the in-process history lifecycle job (Decisions
// 9, 28): on a cron schedule it applies each space's max-count / max-age
// policy to page revisions, logging to history_cleanup_logs only when a run
// actually deleted something. It also drops expired session rows.
package historycleanup

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
)

type Job struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NOTE: the run lock is a session-level advisory lock, which needs a direct
// Postgres session. Behind PgBouncer in transaction-pooling mode it would not
// hold; use session pooling (or a direct connection) for the api.

func New(pool *pgxpool.Pool, logger *slog.Logger) *Job { return &Job{pool: pool, logger: logger} }

// Start schedules the job per spec (5-field cron, @daily, @every 6h; "off" or
// "" disables) and returns a stop function that waits for a running run.
func (j *Job) Start(spec string) (stop func(), err error) {
	if spec == "" || spec == "off" {
		j.logger.Info("history cleanup scheduler disabled")
		return func() {}, nil
	}
	// Runs derive from rootCtx so shutdown cancels an in-flight pass.
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	c := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)))
	if _, err := c.AddFunc(spec, func() {
		ctx, cancel := context.WithTimeout(rootCtx, 30*time.Minute)
		defer cancel()
		if err := j.RunOnce(ctx); err != nil {
			j.logger.Error("history cleanup run failed", "error", err)
		}
	}); err != nil {
		cancelRoot()
		return nil, fmt.Errorf("HISTORY_CLEANUP_SCHEDULE %q: %w", spec, err)
	}
	c.Start()
	j.logger.Info("history cleanup scheduled", "schedule", spec)
	return func() {
		cancelRoot()
		<-c.Stop().Done()
	}, nil
}

// lockKey is the session-level advisory lock that makes one replica win each
// scheduled tick (Decision 28: every replica runs the in-process scheduler).
const lockKey = "lde:history-cleanup"

// RunOnce performs one cleanup pass; a no-op (nil) when another replica holds
// the run lock. The lock is session-scoped on a dedicated connection so each
// space can be pruned in its own short transaction: one failing space doesn't
// roll back the others, and no transaction stays open for the whole pass.
func (j *Job) RunOnce(ctx context.Context) error {
	conn, err := j.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// Registered first so it runs after the unlock below. Hijack() (on the
	// failure path) makes Release a no-op.
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, lockKey).Scan(&got); err != nil {
		return err
	}
	if !got {
		j.logger.Debug("history cleanup: another replica is running it")
		return nil
	}
	defer func() {
		// Background context: still release the lock if ctx was cancelled. If
		// that fails, close the session (which frees its locks) instead of
		// returning a lock-holding connection to the pool.
		if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext($1))`, lockKey); err != nil {
			j.logger.Warn("history cleanup: releasing advisory lock failed; closing the connection", "error", err)
			_ = conn.Hijack().Close(context.Background())
		}
	}()

	spaces, err := db.New(j.pool).ListSpacesWithHistoryPolicy(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, sp := range spaces {
		if err := j.pruneSpace(ctx, sp); err != nil {
			j.logger.Error("history cleanup: pruning a space failed", "space_id", db.FromPgUUID(sp.ID), "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if n, err := db.New(j.pool).DeleteExpiredSessions(ctx); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	} else if n > 0 {
		j.logger.Info("deleted expired session rows", "deleted", n)
	}
	return firstErr
}

// pruneSpace applies one space's policy and logs only an actual deletion
// (Decision 9); delete and log commit together.
func (j *Job) pruneSpace(ctx context.Context, sp db.Space) error {
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	deleted, err := q.PruneSpaceRevisions(ctx, db.PruneSpaceRevisionsParams{
		SpaceID: sp.ID, MaxCount: sp.HistoryMaxCount, MaxAgeDays: sp.HistoryMaxAgeDays,
	})
	if err != nil {
		return err
	}
	if len(deleted) == 0 {
		return nil // Decision 9: no-op runs are not logged
	}
	oldest, newest := deleted[0].CreatedAt, deleted[0].CreatedAt
	for _, d := range deleted[1:] {
		if d.CreatedAt.Time.Before(oldest.Time) {
			oldest = d.CreatedAt
		}
		if d.CreatedAt.Time.After(newest.Time) {
			newest = d.CreatedAt
		}
	}
	if err := q.CreateHistoryCleanupLog(ctx, db.CreateHistoryCleanupLogParams{
		SpaceID: sp.ID, DeletedCount: int32(len(deleted)), OldestDeletedAt: oldest, NewestDeletedAt: newest,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	j.logger.Info("history cleanup deleted revisions", "space_id", db.FromPgUUID(sp.ID),
		"deleted", len(deleted), "oldest", oldest.Time, "newest", newest.Time)
	return nil
}
