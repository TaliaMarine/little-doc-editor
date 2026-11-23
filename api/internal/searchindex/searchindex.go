// Package searchindex extracts plain text from ProseMirror/Tiptap JSON
// (Decision 4) and maintains pages.search_vector through it — the
// SearchIndexer/SearchQuerier boundary SPEC.md §6.8 calls for, so a future
// swap to Meilisearch/Typesense only touches this package, not callers.
package searchindex

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/TaliaMarine/little-doc-editor/api/internal/db"
)

// proseMirrorNode mirrors just enough of the ProseMirror JSON schema to
// walk it for text content — attrs/marks and everything else are ignored,
// since they don't affect what's searchable.
type proseMirrorNode struct {
	Type    string            `json:"type"`
	Text    string            `json:"text"`
	Content []proseMirrorNode `json:"content"`
}

// ExtractPlainText flattens a ProseMirror document's text nodes into a
// single space-separated string suitable for to_tsvector.
func ExtractPlainText(content []byte) string {
	var root proseMirrorNode
	if err := json.Unmarshal(content, &root); err != nil {
		return ""
	}
	var sb strings.Builder
	walk(root, &sb)
	return strings.TrimSpace(sb.String())
}

func walk(n proseMirrorNode, sb *strings.Builder) {
	if n.Text != "" {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(n.Text)
	}
	for _, c := range n.Content {
		walk(c, sb)
	}
}

type Indexer struct {
	queries *db.Queries
}

func NewIndexer(queries *db.Queries) *Indexer {
	return &Indexer{queries: queries}
}

// IndexPage re-derives and stores pageID's search_vector from content.
// Callers should invoke this after every page create/content update — it's
// not triggered automatically in SQL (see the package doc).
func (idx *Indexer) IndexPage(ctx context.Context, pageID uuid.UUID, content []byte) error {
	text := ExtractPlainText(content)
	return idx.queries.UpdatePageSearchVector(ctx, db.UpdatePageSearchVectorParams{
		ID:         db.ToPgUUID(pageID),
		ToTsvector: []byte(text),
	})
}

type Result struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

type Querier struct {
	queries *db.Queries
}

func NewQuerier(queries *db.Queries) *Querier {
	return &Querier{queries: queries}
}

func (q *Querier) Search(ctx context.Context, spaceID uuid.UUID, query string, limit int32) ([]Result, error) {
	pages, err := q.queries.SearchPages(ctx, db.SearchPagesParams{
		SpaceID:            db.ToPgUUID(spaceID),
		WebsearchToTsquery: query,
		Limit:              limit,
	})
	if err != nil {
		return nil, err
	}

	results := make([]Result, len(pages))
	for i, p := range pages {
		results[i] = Result{ID: db.FromPgUUID(p.ID), Title: p.Title}
	}
	return results, nil
}
