//go:build sqlite_fts5

package memory_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errorEmbedder struct{}

func (emb *errorEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, fmt.Errorf("embedding service unavailable")
}

func TestHybridSearcher_Search_EmbedderError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)
	episodeStore := memory.NewEpisodeStore(db)

	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, &errorEmbedder{}, 0.5, 0.5)

	_, err := searcher.Search(context.Background(), "test query", 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector search")
}

func TestHybridSearcher_VectorSearch_EmbedderError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)
	episodeStore := memory.NewEpisodeStore(db)

	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, &errorEmbedder{}, 1.0, 0.0)

	_, err := searcher.Search(context.Background(), "query", 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "embedding")
}

func TestHybridSearcher_VectorSearch_EpisodesLoadError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0}}

	require.NoError(t, db.Close())

	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 1.0, 0.0)

	_, err := searcher.Search(context.Background(), "query", 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector search")
}

func TestHybridSearcher_KeywordSearch_FactsError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0}}

	_, err := db.RawDB().Exec(`DROP TABLE facts_fts`)
	require.NoError(t, err)

	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.0, 1.0)

	_, err = searcher.Search(context.Background(), "test query", 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyword search")
}

func TestHybridSearcher_KeywordSearch_EpisodesError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0}}

	_, err := db.RawDB().Exec(`DROP TABLE episodes_fts`)
	require.NoError(t, err)

	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.0, 1.0)

	_, err = searcher.Search(context.Background(), "test query", 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyword search")
}

func TestHybridSearcher_KeywordSearch_BeliefsError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0}}

	_, err := db.RawDB().Exec(`DROP TABLE beliefs_fts`)
	require.NoError(t, err)

	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.0, 1.0)

	_, err = searcher.Search(context.Background(), "test query", 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyword search")
}

func TestNormaliseKeywordScore_MaxScoreZero_ReturnsZero(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	embedder := &fakeEmbedder{vector: []float32{0.0, 0.0}}

	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.0, 1.0)

	results, err := searcher.Search(context.Background(), "nonexistent terms xyzzy", 10)

	require.NoError(t, err)
	assert.Empty(t, results)
}
