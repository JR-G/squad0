package memory

import (
	"context"
	"fmt"
	"strings"
)

// ConfirmOrCreate finds an existing belief with similar content using
// FTS5 keyword matching. If found, it confirms the belief (bumps
// confirmations and confidence). If not found, it creates a new belief.
// This avoids depending on Ollama for vector search.
func (store *FactStore) ConfirmOrCreate(ctx context.Context, content, sourceOutcome string) error {
	match := store.findSimilarBelief(ctx, content)

	if match > 0 {
		return store.ConfirmBelief(ctx, match)
	}

	_, createErr := store.CreateBelief(ctx, Belief{
		Content:       content,
		Confidence:    0.4,
		Confirmations: 1,
		SourceOutcome: sourceOutcome,
	})
	return createErr
}

// findSimilarBelief uses FTS5 to find a belief with overlapping content.
// Returns the ID of the best match, or 0 if none found.
func (store *FactStore) findSimilarBelief(ctx context.Context, content string) int64 {
	keywords := extractKeywords(content)
	if keywords == "" {
		return 0
	}

	var matchID int64
	err := store.db.RawDB().QueryRowContext(ctx,
		`SELECT rowid FROM beliefs_fts WHERE beliefs_fts MATCH ? LIMIT 1`,
		keywords,
	).Scan(&matchID)
	if err != nil {
		return 0
	}

	return matchID
}

// extractKeywords pulls significant words from text for FTS matching.
// Strips common words and returns space-separated terms.
func extractKeywords(text string) string {
	stopWords := map[string]bool{
		"i": true, "we": true, "the": true, "a": true, "an": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"to": true, "of": true, "and": true, "or": true, "in": true,
		"on": true, "at": true, "it": true, "that": true, "this": true,
		"for": true, "with": true, "not": true, "do": true, "does": true,
		"should": true, "think": true, "believe": true, "always": true,
		"never": true, "need": true, "have": true, "has": true,
	}

	words := strings.Fields(strings.ToLower(text))
	significant := make([]string, 0, len(words))

	for _, word := range words {
		cleaned := strings.Trim(word, ".,;:!?\"'()-")
		if len(cleaned) < 3 {
			continue
		}
		if stopWords[cleaned] {
			continue
		}
		significant = append(significant, cleaned)
	}

	if len(significant) > 5 {
		significant = significant[:5]
	}

	return strings.Join(significant, " OR ")
}

// SearchBeliefsByKeyword uses FTS5 to find beliefs containing the
// given keyword. Returns the content of matching beliefs.
func (store *FactStore) SearchBeliefsByKeyword(ctx context.Context, keyword string, limit int) ([]string, error) {
	sanitised := sanitiseBeliefQuery(keyword)
	if sanitised == "" {
		return nil, nil
	}

	rows, err := store.db.RawDB().QueryContext(ctx,
		`SELECT b.content FROM beliefs b
		 JOIN beliefs_fts fts ON b.id = fts.rowid
		 WHERE beliefs_fts MATCH ?
		 LIMIT ?`,
		sanitised, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("searching beliefs for %q: %w", keyword, err)
	}
	defer func() { _ = rows.Close() }()

	var results []string
	for rows.Next() {
		var content string
		if scanErr := rows.Scan(&content); scanErr != nil {
			return nil, fmt.Errorf("scanning belief content: %w", scanErr)
		}
		results = append(results, content)
	}

	return results, rows.Err()
}

// sanitiseBeliefQuery splits a keyword into individual terms and
// joins them with OR for broader matching. Each term is quoted to
// prevent FTS5 syntax injection.
func sanitiseBeliefQuery(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return ""
	}

	// Split on hyphens too for ticket IDs like "JAM-42".
	var terms []string
	for _, word := range words {
		parts := strings.Split(word, "-")
		for _, part := range parts {
			cleaned := strings.Map(func(r rune) rune {
				if r == '"' || r == '\'' || r == '(' || r == ')' || r == '*' || r == ':' {
					return -1
				}
				return r
			}, part)
			if cleaned == "" {
				continue
			}
			terms = append(terms, `"`+cleaned+`"`)
		}
	}

	if len(terms) == 0 {
		return ""
	}

	return strings.Join(terms, " OR ")
}
