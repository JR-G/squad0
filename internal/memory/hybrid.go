package memory

import (
	"context"
	"fmt"
	"log"
	"sort"
)

// TextEmbedder generates embedding vectors from text.
type TextEmbedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// ScoredResult represents a memory item with its fused relevance score.
type ScoredResult struct {
	ID         int64
	Table      string
	Content    string
	FinalScore float64
}

// HybridSearcher combines vector similarity and BM25 keyword search
// into a single ranked result set.
type HybridSearcher struct {
	ftsStore      *FTSStore
	episodeStore  *EpisodeStore
	embedder      TextEmbedder
	vectorWeight  float64
	keywordWeight float64
}

// NewHybridSearcher creates a HybridSearcher with the given score weights.
func NewHybridSearcher(
	ftsStore *FTSStore,
	episodeStore *EpisodeStore,
	embedder TextEmbedder,
	vectorWeight float64,
	keywordWeight float64,
) *HybridSearcher {
	return &HybridSearcher{
		ftsStore:      ftsStore,
		episodeStore:  episodeStore,
		embedder:      embedder,
		vectorWeight:  vectorWeight,
		keywordWeight: keywordWeight,
	}
}

type scoreKey struct {
	table string
	id    int64
}

// Search performs hybrid search: embeds the query, runs brute-force
// vector similarity against episodes, runs BM25 keyword search across
// all tables, fuses the scores, and returns the top results.
func (searcher *HybridSearcher) Search(ctx context.Context, query string, limit int) ([]ScoredResult, error) {
	scores := make(map[scoreKey]*ScoredResult)

	vectorResults, err := searcher.vectorSearch(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	for _, result := range vectorResults {
		key := scoreKey{table: result.Table, id: result.ID}
		scores[key] = &ScoredResult{
			ID:         result.ID,
			Table:      result.Table,
			Content:    result.Content,
			FinalScore: searcher.vectorWeight * result.FinalScore,
		}
	}

	keywordResults, err := searcher.keywordSearch(ctx, query, limit*3)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}

	maxBM25 := maxKeywordScore(keywordResults)

	for _, result := range keywordResults {
		normalisedScore := normaliseKeywordScore(result.Score, maxBM25)
		key := scoreKey{table: result.Table, id: result.RowID}
		mergeKeywordScore(scores, key, result, searcher.keywordWeight*normalisedScore)
	}

	ranked := make([]ScoredResult, 0, len(scores))
	for _, result := range scores {
		ranked = append(ranked, *result)
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].FinalScore > ranked[j].FinalScore
	})

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	return ranked, nil
}

func (searcher *HybridSearcher) vectorSearch(ctx context.Context, query string) ([]ScoredResult, error) {
	queryVec, err := searcher.embedder.Embed(ctx, query)
	if err != nil {
		log.Printf("vector search skipped (embedder unavailable): %v", err)
		return nil, nil
	}

	episodes, err := searcher.episodeStore.EpisodesWithEmbeddings(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading episodes: %w", err)
	}

	results := make([]ScoredResult, 0, len(episodes))
	for _, episode := range episodes {
		similarity := CosineSimilarity(queryVec, episode.Embedding)
		normalised := (float64(similarity) + 1.0) / 2.0

		results = append(results, ScoredResult{
			ID:         episode.ID,
			Table:      "episodes",
			Content:    episode.Summary,
			FinalScore: normalised,
		})
	}

	return results, nil
}

func (searcher *HybridSearcher) keywordSearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	factResults, err := searcher.ftsStore.SearchFacts(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("searching facts: %w", err)
	}

	episodeResults, err := searcher.ftsStore.SearchEpisodes(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("searching episodes: %w", err)
	}

	beliefResults, err := searcher.ftsStore.SearchBeliefs(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("searching beliefs: %w", err)
	}

	var combined []SearchResult
	combined = append(combined, factResults...)
	combined = append(combined, episodeResults...)
	combined = append(combined, beliefResults...)

	return combined, nil
}

func normaliseKeywordScore(score, maxScore float64) float64 {
	if maxScore <= 0 {
		return 0
	}
	return score / maxScore
}

func mergeKeywordScore(scores map[scoreKey]*ScoredResult, key scoreKey, result SearchResult, weightedScore float64) {
	existing, ok := scores[key]
	if ok {
		existing.FinalScore += weightedScore
		return
	}

	scores[key] = &ScoredResult{
		ID:         result.RowID,
		Table:      result.Table,
		FinalScore: weightedScore,
	}
}

func maxKeywordScore(results []SearchResult) float64 {
	maxScore := 0.0
	for _, result := range results {
		if result.Score > maxScore {
			maxScore = result.Score
		}
	}
	return maxScore
}
