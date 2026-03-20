package memory

import (
	"context"
	"fmt"
	"strings"
)

// SearchResult represents a single FTS5 match with its BM25 relevance score.
type SearchResult struct {
	RowID int64
	Score float64
	Table string
}

// FTSStore provides full-text search operations across the knowledge
// graph tables.
type FTSStore struct {
	db *DB
}

// NewFTSStore creates an FTSStore backed by the given database.
func NewFTSStore(db *DB) *FTSStore {
	return &FTSStore{db: db}
}

// SearchFacts performs a BM25-ranked keyword search across facts.
func (store *FTSStore) SearchFacts(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return store.search(ctx, "facts_fts", "facts", query, limit)
}

// SearchEpisodes performs a BM25-ranked keyword search across episodes.
func (store *FTSStore) SearchEpisodes(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return store.search(ctx, "episodes_fts", "episodes", query, limit)
}

// SearchBeliefs performs a BM25-ranked keyword search across beliefs.
func (store *FTSStore) SearchBeliefs(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return store.search(ctx, "beliefs_fts", "beliefs", query, limit)
}

func (store *FTSStore) search(ctx context.Context, ftsTable, sourceTable, query string, limit int) ([]SearchResult, error) {
	sanitised := sanitiseQuery(query)
	if sanitised == "" {
		return nil, nil
	}

	sql := fmt.Sprintf(
		`SELECT rowid, -bm25(%s) AS score FROM %s WHERE %s MATCH ? ORDER BY score DESC LIMIT ?`,
		ftsTable, ftsTable, ftsTable,
	)

	rows, err := store.db.RawDB().QueryContext(ctx, sql, sanitised, limit)
	if err != nil {
		return nil, fmt.Errorf("searching %s: %w", ftsTable, err)
	}
	defer func() { _ = rows.Close() }()

	var results []SearchResult
	for rows.Next() {
		var result SearchResult
		if err := rows.Scan(&result.RowID, &result.Score); err != nil {
			return nil, fmt.Errorf("scanning %s result: %w", ftsTable, err)
		}
		result.Table = sourceTable
		results = append(results, result)
	}

	return results, rows.Err()
}

// sanitiseQuery converts a free-text query into safe FTS5 syntax by
// quoting each term to prevent syntax injection.
func sanitiseQuery(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return ""
	}

	quoted := make([]string, 0, len(words))
	for _, word := range words {
		cleaned := strings.Map(func(r rune) rune {
			if r == '"' || r == '\'' || r == '(' || r == ')' || r == '*' || r == ':' {
				return -1
			}
			return r
		}, word)
		if cleaned == "" {
			continue
		}
		quoted = append(quoted, `"`+cleaned+`"`)
	}

	return strings.Join(quoted, " ")
}
