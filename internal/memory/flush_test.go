package memory_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEmbedder(t *testing.T) *memory.Embedder {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := map[string][]float32{"embedding": {0.1, 0.2, 0.3}}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	t.Cleanup(server.Close)

	return memory.NewEmbedder(server.URL, "test-model")
}

func TestFlushLearnings_StoresFactsAndBeliefs(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	embedder := newTestEmbedder(t)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Entities: []memory.ExtractedEntity{
			{Name: "payments", Type: "module"},
		},
		Facts: []memory.ExtractedFact{
			{EntityName: "payments", EntityType: "module", Content: "needs retry logic", FactType: "observation"},
		},
		Beliefs: []memory.ExtractedBelief{
			{Content: "explicit error handling is important"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "engineer-1", "SQ-42", embedder)

	require.NoError(t, err)

	entity, err := graphStore.FindEntityByName(ctx, memory.EntityModule, "payments")
	require.NoError(t, err)

	facts, err := factStore.FactsByEntity(ctx, entity.ID)
	require.NoError(t, err)
	assert.Len(t, facts, 1)
	assert.Equal(t, "needs retry logic", facts[0].Content)

	beliefs, err := factStore.TopBeliefs(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, beliefs, 1)
}

func TestFlushLearnings_CreatesEpisodeWithEmbedding(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	embedder := newTestEmbedder(t)
	ctx := context.Background()

	learnings := memory.SessionLearnings{
		Facts: []memory.ExtractedFact{
			{EntityName: "auth", EntityType: "module", Content: "uses JWT", FactType: "observation"},
		},
	}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "engineer-1", "SQ-43", embedder)

	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "engineer-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.NotNil(t, episodes[0].Embedding)
}

func TestFlushLearnings_NilEmbedder_StillWorks(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	learnings := memory.SessionLearnings{}

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, learnings, "pm", "SQ-1", nil)

	require.NoError(t, err)
}

func TestFlushLearnings_EmptyLearnings_CreatesEpisode(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ctx := context.Background()

	err := memory.FlushLearnings(ctx, graphStore, factStore, episodeStore, memory.SessionLearnings{}, "pm", "SQ-1", nil)

	require.NoError(t, err)

	episodes, err := episodeStore.EpisodesByAgent(ctx, "pm")
	require.NoError(t, err)
	assert.Len(t, episodes, 1)
	assert.Contains(t, episodes[0].Summary, "no extracted learnings")
}

func TestParseLearningsJSON_ValidJSON_ReturnsLearnings(t *testing.T) {
	t.Parallel()

	jsonStr := `{
		"facts": [{"entity_name": "auth", "entity_type": "module", "content": "uses JWT", "fact_type": "observation"}],
		"beliefs": [{"content": "testing is good"}],
		"entities": [{"name": "auth", "type": "module"}]
	}`

	learnings, err := memory.ParseLearningsJSON(jsonStr)

	require.NoError(t, err)
	assert.Len(t, learnings.Facts, 1)
	assert.Len(t, learnings.Beliefs, 1)
	assert.Len(t, learnings.Entities, 1)
}

func TestParseLearningsJSON_InvalidJSON_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := memory.ParseLearningsJSON("not json")

	require.Error(t, err)
}

func TestDefaultFlushConfig_ReturnsDefaults(t *testing.T) {
	t.Parallel()

	cfg := memory.DefaultFlushConfig()

	assert.Equal(t, 10, cfg.MaxFactsPerSession)
	assert.Equal(t, 5, cfg.MaxBeliefsPerSession)
}
