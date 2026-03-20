package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRetriever(t *testing.T) (*memory.Retriever, *memory.DB) {
	t.Helper()
	db := openTestDB(t)

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0, 0.0}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)

	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	return retriever, db
}

func TestRetriever_Retrieve_EmptyDB_ReturnsEmptyContext(t *testing.T) {
	t.Parallel()

	retriever, _ := setupRetriever(t)

	result, err := retriever.Retrieve(context.Background(), "implement auth", nil)

	require.NoError(t, err)
	assert.Empty(t, result.Facts)
	assert.Empty(t, result.Beliefs)
	assert.Empty(t, result.Episodes)
	assert.Empty(t, result.Entities)
}

func TestRetriever_Retrieve_FindsEpisodesByKeyword(t *testing.T) {
	t.Parallel()

	retriever, db := setupRetriever(t)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "Implemented payment retry logic",
		Embedding: []float32{0.9, 0.1, 0.0}, Outcome: memory.OutcomeSuccess,
	})

	result, err := retriever.Retrieve(ctx, "payment retry", nil)

	require.NoError(t, err)
	assert.NotEmpty(t, result.Episodes)
}

func TestRetriever_Retrieve_FindsFactsByGraphTraversal(t *testing.T) {
	t.Parallel()

	retriever, db := setupRetriever(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "payments", Summary: "payment processing",
	})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "Stripe webhooks need idempotency keys",
		Type: memory.FactWarning, Confidence: 0.8, Confirmations: 1,
	})

	result, err := retriever.Retrieve(ctx, "fix the payments module", nil)

	require.NoError(t, err)
	assert.NotEmpty(t, result.Facts)
	assert.NotEmpty(t, result.Entities)
}

func TestRetriever_Retrieve_FindsFactsViaRelatedEntities(t *testing.T) {
	t.Parallel()

	retriever, db := setupRetriever(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	paymentsID, _ := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "payments",
	})
	stripeID, _ := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "stripe",
	})
	_, _ = graphStore.CreateRelationship(ctx, memory.Relationship{
		SourceID: paymentsID, TargetID: stripeID,
		Type: memory.RelationDependsOn, Confidence: 0.9,
	})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: stripeID, Content: "Stripe API has rate limits",
		Type: memory.FactWarning, Confidence: 0.7, Confirmations: 1,
	})

	result, err := retriever.Retrieve(ctx, "update payments module", nil)

	require.NoError(t, err)
	assert.NotEmpty(t, result.Facts)
}

func TestRetriever_Retrieve_DeduplicatesFacts(t *testing.T) {
	t.Parallel()

	retriever, db := setupRetriever(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "auth",
	})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "auth module uses JWT tokens",
		Type: memory.FactObservation, Confidence: 0.8, Confirmations: 1,
	})
	_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "worked on auth JWT tokens",
		Embedding: []float32{0.8, 0.2, 0.0}, Outcome: memory.OutcomeSuccess,
	})

	result, err := retriever.Retrieve(ctx, "auth JWT tokens", nil)

	require.NoError(t, err)
	factIDs := make(map[int64]bool)
	for _, fact := range result.Facts {
		assert.False(t, factIDs[fact.ID], "duplicate fact ID %d", fact.ID)
		factIDs[fact.ID] = true
	}
}

func TestRetriever_Retrieve_UsesFilePaths(t *testing.T) {
	t.Parallel()

	retriever, db := setupRetriever(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityFile, Name: "config",
	})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "config loading is order-dependent",
		Type: memory.FactWarning, Confidence: 0.6, Confirmations: 1,
	})

	result, err := retriever.Retrieve(ctx, "update settings", []string{"internal/config/config.go"})

	require.NoError(t, err)
	assert.NotEmpty(t, result.Facts)
}
