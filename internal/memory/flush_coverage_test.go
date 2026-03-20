//go:build sqlite_fts5

package memory_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryEmbed_NilEmbedder_ReturnsNil(t *testing.T) {
	t.Parallel()

	// Verified via FlushLearnings which calls tryEmbed internally
	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Facts: []memory.ExtractedFact{
			{EntityName: "api", EntityType: "module", Content: "needs auth", FactType: "observation"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-1", nil)
	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "eng-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Nil(t, episodes[0].Embedding)
}

func TestBuildEpisodeSummary_OnlyFacts_NoComma(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Facts: []memory.ExtractedFact{
			{EntityName: "db", EntityType: "module", Content: "fact 1", FactType: "observation"},
			{EntityName: "db", EntityType: "module", Content: "fact 2", FactType: "warning"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-2", nil)
	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "eng-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Contains(t, episodes[0].Summary, "Learned 2 facts")
	assert.NotContains(t, episodes[0].Summary, "beliefs")
	assert.NotContains(t, episodes[0].Summary, "entities")
}

func TestBuildEpisodeSummary_OnlyBeliefs_NoPrefix(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Beliefs: []memory.ExtractedBelief{
			{Content: "testing is essential"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-3", nil)
	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "eng-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Contains(t, episodes[0].Summary, "formed 1 beliefs")
	assert.NotContains(t, episodes[0].Summary, "Learned")
}

func TestBuildEpisodeSummary_OnlyEntities_NoPrefix(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Entities: []memory.ExtractedEntity{
			{Name: "auth", Type: "module"},
			{Name: "api", Type: "module"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-4", nil)
	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "eng-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Contains(t, episodes[0].Summary, "encountered 2 entities")
}

func TestBuildEpisodeSummary_FactsAndBeliefs_CommaSeparated(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Facts: []memory.ExtractedFact{
			{EntityName: "db", EntityType: "module", Content: "uses WAL", FactType: "observation"},
		},
		Beliefs: []memory.ExtractedBelief{
			{Content: "WAL mode is essential"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-5", nil)
	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "eng-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Contains(t, episodes[0].Summary, "Learned 1 facts")
	assert.Contains(t, episodes[0].Summary, ", formed 1 beliefs")
}

func TestBuildEpisodeSummary_AllThree_CommaSeparated(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Facts: []memory.ExtractedFact{
			{EntityName: "cache", EntityType: "module", Content: "uses Redis", FactType: "observation"},
		},
		Beliefs: []memory.ExtractedBelief{
			{Content: "caching improves latency"},
		},
		Entities: []memory.ExtractedEntity{
			{Name: "cache", Type: "module"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-6", nil)
	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "eng-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Contains(t, episodes[0].Summary, "Learned 1 facts")
	assert.Contains(t, episodes[0].Summary, ", formed 1 beliefs")
	assert.Contains(t, episodes[0].Summary, ", encountered 1 entities")
}

func TestBuildEpisodeSummary_Empty_ReturnsDefaultMessage(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, memory.SessionLearnings{}, "eng-1", "SQ-7", nil)
	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "eng-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Equal(t, "Session completed with no extracted learnings", episodes[0].Summary)
}

func TestFlushLearnings_EntityCreationError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	// Close the DB to force entity creation to fail
	require.NoError(t, db.Close())

	learnings := memory.SessionLearnings{
		Entities: []memory.ExtractedEntity{
			{Name: "auth", Type: "module"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-8", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating entity")
}

func TestFlushLearnings_FactEntityError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	require.NoError(t, db.Close())

	learnings := memory.SessionLearnings{
		Facts: []memory.ExtractedFact{
			{EntityName: "x", EntityType: "module", Content: "broken", FactType: "observation"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-9", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating entity for fact")
}

func TestFlushLearnings_BeliefCreationError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	// Create entities/facts first, then sabotage beliefs table
	learnings := memory.SessionLearnings{
		Beliefs: []memory.ExtractedBelief{
			{Content: "this will fail"},
		},
	}

	// Drop beliefs table
	_, err := db.RawDB().Exec(`DROP TABLE beliefs`)
	require.NoError(t, err)

	err = memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-10", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing belief")
}

func TestFlushLearnings_EpisodeCreationError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	// Drop episodes table
	_, err := db.RawDB().Exec(`DROP TABLE episodes`)
	require.NoError(t, err)

	learnings := memory.SessionLearnings{}

	err = memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-11", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing episode")
}

func TestFlushLearnings_FactStorageError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	// Create the entity first so FindOrCreateEntity works
	_, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "existing",
	})
	require.NoError(t, err)

	// Drop facts table so CreateFact fails
	_, err = db.RawDB().Exec(`DROP TABLE facts`)
	require.NoError(t, err)

	learnings := memory.SessionLearnings{
		Facts: []memory.ExtractedFact{
			{EntityName: "existing", EntityType: "module", Content: "will fail", FactType: "observation"},
		},
	}

	err = memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-12", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing fact")
}

func TestFlushLearnings_WithEmbedder_StoresEmbedding(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	embedder := newTestEmbedder(t)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Facts: []memory.ExtractedFact{
			{EntityName: "srv", EntityType: "module", Content: "uses gRPC", FactType: "observation"},
		},
		Beliefs: []memory.ExtractedBelief{
			{Content: "gRPC is fast"},
		},
		Entities: []memory.ExtractedEntity{
			{Name: "srv", Type: "module"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-2", "SQ-13", embedder)
	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "eng-2")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.NotNil(t, episodes[0].Embedding)
}

func TestTryEmbed_EmbedderError_ReturnsNil(t *testing.T) {
	t.Parallel()

	// Create an embedder that always returns an error (server returns 500)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	failingEmbedder := memory.NewEmbedder(server.URL, "test-model")

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Facts: []memory.ExtractedFact{
			{EntityName: "svc", EntityType: "module", Content: "embed fails", FactType: "observation"},
		},
	}

	// FlushLearnings should succeed even when embedder errors — tryEmbed
	// swallows the error and returns nil
	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "eng-1", "SQ-99", failingEmbedder)
	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "eng-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Nil(t, episodes[0].Embedding)
}

func TestParseLearningsJSON_EmptyObject_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	learnings, err := memory.ParseLearningsJSON("{}")

	require.NoError(t, err)
	assert.Empty(t, learnings.Facts)
	assert.Empty(t, learnings.Beliefs)
	assert.Empty(t, learnings.Entities)
}

func TestParseLearningsJSON_EmptyArrays_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	learnings, err := memory.ParseLearningsJSON(`{"facts":[],"beliefs":[],"entities":[]}`)

	require.NoError(t, err)
	assert.Empty(t, learnings.Facts)
	assert.Empty(t, learnings.Beliefs)
	assert.Empty(t, learnings.Entities)
}
