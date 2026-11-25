// Package revisions creates immutable page revisions (SPEC.md §6.7).
//
// Rule (recorded in SPEC.md): every content save may create a revision, but
// not every save does. A save is skipped when
//   - the page has history disabled (pages.history_enabled = false), or
//   - the content and title equal the page's latest revision, or
//   - the latest revision was created by the same author less than the
//     debounce interval ago (default 5 minutes, HISTORY_REVISION_DEBOUNCE).
//
// Revisions are immutable, so a skipped save is simply not in history; the
// page's current state is always pages.content. Events that must never be
// lost (page creation, the pre-restore state, a restore) force a revision.
// collab applies the same rule when it persists live content (see
// collab/README.md): same table, same skip conditions.
package revisions

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
)

type Author struct {
	UserID  pgtype.UUID
	GuestID pgtype.UUID
}

type Recorder struct {
	debounce time.Duration
}

func NewRecorder(debounce time.Duration) *Recorder { return &Recorder{debounce: debounce} }

func historyOff(p db.Page) bool { return p.HistoryEnabled != nil && !*p.HistoryEnabled }

func sameBytes(a, b []byte) bool { return bytes.Equal(a, b) }

func (r *Recorder) create(ctx context.Context, q *db.Queries, p db.Page, a Author) error {
	title := p.Title
	_, err := q.CreatePageRevision(ctx, db.CreatePageRevisionParams{
		PageID: p.ID, Content: p.Content, Title: &title,
		AuthorUserID: a.UserID, AuthorGuestID: a.GuestID,
	})
	return err
}

// Force records p's current state unconditionally (still honouring
// history_enabled = false).
func (r *Recorder) Force(ctx context.Context, q *db.Queries, p db.Page, a Author) error {
	if historyOff(p) {
		return nil
	}
	return r.create(ctx, q, p, a)
}

// OnSave is called after a content save: before is the page as it was, after
// the page as updated (both as read back from Postgres, so jsonb content is
// normalized and byte-comparable).
func (r *Recorder) OnSave(ctx context.Context, q *db.Queries, before, after db.Page, a Author) error {
	if historyOff(after) {
		return nil
	}
	latest, err := q.GetLatestPageRevision(ctx, after.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		// First revision of a page that predates history: keep the state it
		// had before this save as the baseline, so the first edit is undoable.
		if !sameBytes(before.Content, after.Content) || before.Title != after.Title {
			if err := r.create(ctx, q, before, Author{UserID: before.CreatedByUserID, GuestID: before.CreatedByGuestID}); err != nil {
				return err
			}
		}
		return r.create(ctx, q, after, a)
	}
	if err != nil {
		return err
	}
	if sameBytes(latest.Content, after.Content) && latest.Title != nil && *latest.Title == after.Title {
		return nil
	}
	if r.debounce > 0 && sameAuthor(latest, a) && time.Since(latest.CreatedAt.Time) < r.debounce {
		return nil
	}
	// A different author is saving over content the previous author wrote
	// after their last recorded revision (their saves inside the debounce
	// window were skipped): record that state under the previous author, so
	// it isn't later attributed to whoever saved next.
	if !sameAuthor(latest, a) && !sameBytes(latest.Content, before.Content) {
		if err := r.create(ctx, q, before, Author{UserID: latest.AuthorUserID, GuestID: latest.AuthorGuestID}); err != nil {
			return err
		}
	}
	return r.create(ctx, q, after, a)
}

func sameAuthor(rev db.PageRevision, a Author) bool {
	if rev.AuthorUserID.Valid || a.UserID.Valid {
		return rev.AuthorUserID == a.UserID
	}
	return rev.AuthorGuestID.Valid && rev.AuthorGuestID == a.GuestID
}
