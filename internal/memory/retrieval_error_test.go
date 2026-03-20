//go:build sqlite_fts5

package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetriever_RetrieveBySearch_GetFactError_Continues(t *testing.T) {
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

	entityID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "skip-test",
	})
	require.NoError(t, err)

	factID, err := factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "searchable fact for retrieval skip test",
		Type: memory.FactObservation, Confidence: 0.8, Confirmations: 1,
	})
	require.NoError(t, err)

	_, err = db.RawDB().Exec(`DELETE FROM facts WHERE id = ?`, factID)
	require.NoError(t, err)

	result, err := retriever.Retrieve(ctx, "searchable retrieval skip test", nil)

	require.NoError(t, err)
	_ = result
}

func TestRetriever_RetrieveBySearch_GetBeliefError_Continues(t *testing.T) {
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

	beliefID, err := factStore.CreateBelief(ctx, memory.Belief{
		Content: "searchable belief for retrieval skip test", Confidence: 0.8,
	})
	require.NoError(t, err)

	_, err = db.RawDB().Exec(`DELETE FROM beliefs WHERE id = ?`, beliefID)
	require.NoError(t, err)

	result, err := retriever.Retrieve(ctx, "searchable belief retrieval skip test", nil)

	require.NoError(t, err)
	_ = result
}

func TestRetriever_RetrieveBySearch_GetEpisodeError_Continues(t *testing.T) {
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

	epID, err := episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "searchable episode for retrieval skip test",
		Embedding: []float32{0.9, 0.1, 0.0}, Outcome: memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	_, err = db.RawDB().Exec(`DELETE FROM episodes WHERE id = ?`, epID)
	require.NoError(t, err)

	result, err := retriever.Retrieve(ctx, "searchable episode retrieval skip test", nil)

	require.NoError(t, err)
	_ = result
}

func TestRetriever_CollectGraphContext_EntityWithRelatedFacts(t *testing.T) {
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

	entityA, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "graphctx", Summary: "primary entity",
	})
	require.NoError(t, err)

	entityB, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "related-graphctx", Summary: "secondary entity",
	})
	require.NoError(t, err)

	_, err = graphStore.CreateRelationship(ctx, memory.Relationship{
		SourceID: entityA, TargetID: entityB,
		Type: memory.RelationDependsOn, Confidence: 0.8,
	})
	require.NoError(t, err)

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityA, Content: "primary fact for graphctx test",
		Type: memory.FactObservation, Confidence: 0.8, Confirmations: 1,
	})
	require.NoError(t, err)

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityB, Content: "related fact for graphctx test",
		Type: memory.FactWarning, Confidence: 0.7, Confirmations: 1,
	})
	require.NoError(t, err)

	result, err := retriever.Retrieve(ctx, "update graphctx module", nil)

	require.NoError(t, err)
	assert.NotEmpty(t, result.Entities)
	assert.NotEmpty(t, result.Facts)
}

func TestRankAndDedup_LimitsBeliefs(t *testing.T) {
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

	for idx := 0; idx < 10; idx++ {
		_, _ = factStore.CreateBelief(ctx, memory.Belief{
			Content: "belief about limiting test patterns", Confidence: 0.5,
		})
	}

	result, err := retriever.Retrieve(ctx, "limiting test patterns", nil)

	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Beliefs), 2)
}

func TestRankAndDedup_LimitsEpisodes(t *testing.T) {
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

	for idx := 0; idx < 10; idx++ {
		_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
			Agent: "engineer-1", Summary: "episode about limiting test patterns",
			Embedding: []float32{0.9, 0.1, 0.0}, Outcome: memory.OutcomeSuccess,
		})
	}

	result, err := retriever.Retrieve(ctx, "limiting test patterns", nil)

	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Episodes), 2)
}

func TestDedupBeliefs_RemovesDuplicates(t *testing.T) {
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

	_, err := factStore.CreateBelief(ctx, memory.Belief{
		Content: "unique dedup test belief pattern xyzzy", Confidence: 0.9,
	})
	require.NoError(t, err)

	result, err := retriever.Retrieve(ctx, "unique dedup test belief pattern xyzzy", nil)
	require.NoError(t, err)

	seen := make(map[int64]bool)
	for _, belief := range result.Beliefs {
		assert.False(t, seen[belief.ID], "duplicate belief ID %d", belief.ID)
		seen[belief.ID] = true
	}
}

func TestDedupEpisodes_RemovesDuplicates(t *testing.T) {
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

	_, err := episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "unique dedup episode test xyzzy",
		Embedding: []float32{0.9, 0.1, 0.0}, Outcome: memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	result, err := retriever.Retrieve(ctx, "unique dedup episode test xyzzy", nil)
	require.NoError(t, err)

	seen := make(map[int64]bool)
	for _, ep := range result.Episodes {
		assert.False(t, seen[ep.ID], "duplicate episode ID %d", ep.ID)
		seen[ep.ID] = true
	}
}

func TestDedupEntities_RemovesDuplicates(t *testing.T) {
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

	entityID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "dedup-entity-test", Summary: "test",
	})
	require.NoError(t, err)
	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "fact for dedup entity test",
		Type: memory.FactObservation, Confidence: 0.8, Confirmations: 1,
	})
	require.NoError(t, err)

	result, err := retriever.Retrieve(ctx, "dedup-entity-test", nil)
	require.NoError(t, err)

	seen := make(map[int64]bool)
	for _, entity := range result.Entities {
		assert.False(t, seen[entity.ID], "duplicate entity ID %d", entity.ID)
		seen[entity.ID] = true
	}
}
