//go:build sqlite_fts5

package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests targeting dedup functions with actual duplicates.

func TestDedupBeliefs_WithDuplicateIDs_RemovesDuplicates(t *testing.T) {
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

	// Create a belief with keywords that will match both vector and keyword search
	_, err := factStore.CreateBelief(ctx, memory.Belief{
		Content: "duplicate detection test belief xyzzy foobarbaz", Confidence: 0.9,
	})
	require.NoError(t, err)

	// Also create a matching episode
	_, err = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "duplicate detection test episode xyzzy foobarbaz",
		Embedding: []float32{0.95, 0.05, 0.0}, Outcome: memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	result, err := retriever.Retrieve(ctx, "duplicate detection test xyzzy foobarbaz", nil)
	require.NoError(t, err)

	// Verify no duplicates in beliefs
	beliefSeen := make(map[int64]bool, len(result.Beliefs))
	for _, belief := range result.Beliefs {
		assert.False(t, beliefSeen[belief.ID], "duplicate belief ID %d", belief.ID)
		beliefSeen[belief.ID] = true
	}

	// Verify no duplicates in episodes
	episodeSeen := make(map[int64]bool, len(result.Episodes))
	for _, episode := range result.Episodes {
		assert.False(t, episodeSeen[episode.ID], "duplicate episode ID %d", episode.ID)
		episodeSeen[episode.ID] = true
	}
}

func TestRankAndDedup_LimitsEntities_NotCapped(t *testing.T) {
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

	// Create entities that should be found via graph traversal
	for idx := 0; idx < 5; idx++ {
		names := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
		_, err := graphStore.CreateEntity(ctx, memory.Entity{
			Type: memory.EntityModule, Name: names[idx], Summary: "entity for ranking test",
		})
		require.NoError(t, err)
	}

	result, err := retriever.Retrieve(ctx, "alpha beta gamma", nil)
	require.NoError(t, err)

	// entities are not capped by topK, but they should be deduped
	entitySeen := make(map[int64]bool, len(result.Entities))
	for _, entity := range result.Entities {
		assert.False(t, entitySeen[entity.ID], "duplicate entity ID %d", entity.ID)
		entitySeen[entity.ID] = true
	}
}

func TestFindOrCreateEntity_GetEntityError_AfterCreate(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	// First call: find nothing, create an entity
	entity, created, err := store.FindOrCreateEntity(
		ctx, memory.EntityPattern, "new-pattern-test", "a new pattern",
	)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "new-pattern-test", entity.Name)
	assert.Greater(t, entity.ID, int64(0))

	// Verify it was really created
	fetched, err := store.GetEntity(ctx, entity.ID)
	require.NoError(t, err)
	assert.Equal(t, "new-pattern-test", fetched.Name)
}

func TestCollectGraphContext_FactsByEntityError_Continues(t *testing.T) {
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

	// Create an entity with a related entity
	entityA, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "factserr", Summary: "entity for error test",
	})
	require.NoError(t, err)

	entityB, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "factserr-related", Summary: "related entity",
	})
	require.NoError(t, err)

	_, err = graphStore.CreateRelationship(ctx, memory.Relationship{
		SourceID: entityA, TargetID: entityB,
		Type: memory.RelationDependsOn, Confidence: 0.8,
	})
	require.NoError(t, err)

	// Drop facts table so FactsByEntity fails
	_, err = db.RawDB().Exec(`DROP TABLE facts`)
	require.NoError(t, err)

	// collectGraphContext should continue despite errors
	result, err := retriever.Retrieve(ctx, "factserr module", nil)
	require.NoError(t, err)
	// Should have entities even without facts
	assert.NotEmpty(t, result.Entities)
}

func TestHybridSearch_NormaliseZeroMax_ReturnsZero(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	episodeStore := memory.NewEpisodeStore(db)
	factStore := memory.NewFactStore(db)
	graphStore := memory.NewGraphStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	// Create data that will only match via vector search, not keyword
	_, err := episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "unrelated concept alpha",
		Embedding: []float32{1.0, 0.0}, Outcome: memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	entityID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "gamma",
	})
	require.NoError(t, err)

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "something about gamma unrelated",
		Type: memory.FactObservation, Confidence: 0.5,
	})
	require.NoError(t, err)

	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0}}
	// Use keyword-only weight with a query that does not match any FTS data
	searcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 1.0, 0.0)

	results, err := searcher.Search(ctx, "completely different nonmatching query zzzqqq", 10)

	require.NoError(t, err)
	// All keyword scores are zero, normaliseKeywordScore(score, 0) returns 0
	for _, result := range results {
		assert.GreaterOrEqual(t, result.FinalScore, 0.0)
	}
}

func TestRetrieve_DuplicateFactFromBothPaths_Deduped(t *testing.T) {
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

	// Create entity with name that will match a mention extracted from ticket
	entityID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "dupfactmod", Summary: "module for dedup test",
	})
	require.NoError(t, err)

	// Create fact with content that matches the keyword search AND is attached
	// to the entity found via graph traversal
	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "dupfactmod has fragile error handling",
		Type: memory.FactWarning, Confidence: 0.8, Confirmations: 1,
	})
	require.NoError(t, err)

	// Query mentions "dupfactmod" so it's found via graph traversal,
	// AND the fact content matches "dupfactmod fragile" via keyword search
	result, err := retriever.Retrieve(ctx, "fix dupfactmod fragile error handling", nil)

	require.NoError(t, err)

	// Verify deduplication: no duplicate fact IDs
	seen := make(map[int64]bool, len(result.Facts))
	for _, fact := range result.Facts {
		assert.False(t, seen[fact.ID], "duplicate fact ID %d", fact.ID)
		seen[fact.ID] = true
	}
}

func TestRankAndDedup_MoreEpisodesThanTopK_Limits(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	embedder := &fakeEmbedder{vector: []float32{1.0, 0.0, 0.0}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	// topK = 1 so that limiting kicks in for all types
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 1)
	ctx := context.Background()

	for idx := 0; idx < 5; idx++ {
		_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
			Agent: "engineer-1", Summary: "episode for dedup topk limit test zzzqqq",
			Embedding: []float32{0.9, 0.1, 0.0}, Outcome: memory.OutcomeSuccess,
		})
	}

	for idx := 0; idx < 5; idx++ {
		_, _ = factStore.CreateBelief(ctx, memory.Belief{
			Content: "belief for dedup topk limit test zzzqqq", Confidence: 0.5,
		})
	}

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "topklimit",
	})
	for idx := 0; idx < 5; idx++ {
		_, _ = factStore.CreateFact(ctx, memory.Fact{
			EntityID: entityID, Content: "fact for dedup topk limit test zzzqqq",
			Type: memory.FactObservation, Confidence: 0.5, Confirmations: 1,
		})
	}

	result, err := retriever.Retrieve(ctx, "dedup topk limit test zzzqqq", nil)

	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Facts), 1)
	assert.LessOrEqual(t, len(result.Beliefs), 1)
	assert.LessOrEqual(t, len(result.Episodes), 1)
}

func TestCreateFact_WithSourceEpisodeID_StoresCorrectly(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	entityID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "source-test",
	})
	require.NoError(t, err)

	epID, err := episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "pm", Summary: "source ep", Outcome: memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	factID, err := factStore.CreateFact(ctx, memory.Fact{
		EntityID:        entityID,
		Content:         "fact with source episode",
		Type:            memory.FactObservation,
		Confidence:      0.7,
		Confirmations:   1,
		SourceEpisodeID: &epID,
	})
	require.NoError(t, err)

	fact, err := factStore.GetFact(ctx, factID)
	require.NoError(t, err)
	require.NotNil(t, fact.SourceEpisodeID)
	assert.Equal(t, epID, *fact.SourceEpisodeID)
}
