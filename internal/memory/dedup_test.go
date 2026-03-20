package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetriever_Retrieve_DeduplicatesBeliefs(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0, 0.0}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	ctx := context.Background()

	_, _ = factStore.CreateBelief(ctx, memory.Belief{
		Content: "testing with keyword searchable belief", Confidence: 0.9,
	})

	result, err := retriever.Retrieve(ctx, "testing keyword searchable belief", nil)

	require.NoError(t, err)

	beliefIDs := make(map[int64]bool)
	for _, belief := range result.Beliefs {
		assert.False(t, beliefIDs[belief.ID], "duplicate belief ID %d", belief.ID)
		beliefIDs[belief.ID] = true
	}
}

func TestRetriever_Retrieve_LimitsResults(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0, 0.0}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 2)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "test"})
	for i := 0; i < 10; i++ {
		_, _ = factStore.CreateFact(ctx, memory.Fact{
			EntityID: entityID, Content: "fact about test module",
			Type: memory.FactObservation, Confidence: 0.5, Confirmations: 1,
		})
	}

	result, err := retriever.Retrieve(ctx, "test module facts", nil)

	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Facts), 2)
}
