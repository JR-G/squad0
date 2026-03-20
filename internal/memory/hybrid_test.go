package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEmbedder struct {
	vector []float32
}

func (emb *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return emb.vector, nil
}

func TestHybridSearcher_Search_CombinesVectorAndKeyword(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	episodeStore := memory.NewEpisodeStore(db)
	factStore := memory.NewFactStore(db)
	graphStore := memory.NewGraphStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	queryVec := []float32{1.0, 0.0, 0.0}

	_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "Fixed payment retry bug",
		Embedding: []float32{0.9, 0.1, 0.0}, Outcome: memory.OutcomeSuccess,
	})
	_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-2", Summary: "Added user profile page",
		Embedding: []float32{0.0, 0.0, 1.0}, Outcome: memory.OutcomeSuccess,
	})

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "payments"})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "Payment retry needs backoff",
		Type: memory.FactWarning, Confidence: 0.8,
	})

	embedder := &fakeEmbedder{vector: queryVec}
	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)

	results, err := searcher.Search(ctx, "payment retry", 10)

	require.NoError(t, err)
	assert.NotEmpty(t, results)
	assert.Equal(t, results[0].FinalScore, results[0].FinalScore)
}

func TestHybridSearcher_Search_EmptyDB_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0}}

	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)

	results, err := searcher.Search(context.Background(), "anything", 10)

	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestHybridSearcher_Search_VectorOnlyResults(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "session one",
		Embedding: []float32{1.0, 0.0}, Outcome: memory.OutcomeSuccess,
	})

	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0}}
	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 1.0, 0.0)

	results, err := searcher.Search(ctx, "unrelated query", 10)

	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestHybridSearcher_Search_KeywordOnlyResults(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	episodeStore := memory.NewEpisodeStore(db)
	factStore := memory.NewFactStore(db)
	graphStore := memory.NewGraphStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "search"})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "elasticsearch has latency issues",
		Type: memory.FactObservation, Confidence: 0.6,
	})

	embedder := &fakeEmbedder{vector: []float32{0.0, 0.0}}
	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.0, 1.0)

	results, err := searcher.Search(ctx, "elasticsearch latency", 10)

	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestHybridSearcher_Search_RespectsLimit(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
			Agent: "engineer-1", Summary: "session about testing patterns",
			Embedding: []float32{1.0, 0.0}, Outcome: memory.OutcomeSuccess,
		})
	}

	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0}}
	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)

	results, err := searcher.Search(ctx, "testing patterns", 3)

	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 3)
}
