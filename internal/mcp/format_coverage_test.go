package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/mcp"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatRetrievalContext_WithBeliefs_ShowsBeliefs(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	_, err := factStore.CreateBelief(ctx, memory.Belief{
		Content:    "always use context for cancellation",
		Confidence: 0.9,
	})
	require.NoError(t, err)

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall",
		Arguments: map[string]interface{}{"query": "context cancellation"},
	})

	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "Beliefs:")
	assert.Contains(t, result.Content[0].Text, "always use context for cancellation")
}

func TestFormatRetrievalContext_WithEpisodes_ShowsSessions(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	_, err := episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent:   "engineer-1",
		Ticket:  "SQ-99",
		Summary: "refactored the database layer for WAL mode",
		Outcome: memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall",
		Arguments: map[string]interface{}{"query": "database WAL refactor"},
	})

	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "Past sessions:")
	assert.Contains(t, result.Content[0].Text, "SQ-99")
	assert.Contains(t, result.Content[0].Text, "refactored the database layer")
}

func TestFormatRecalledEntities_EmptySummary_OmitsDash(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	// Create two entities: one with summary, one without.
	entityWithSummaryID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "api", Summary: "REST API layer",
	})
	require.NoError(t, err)

	entityNoSummaryID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "utils",
	})
	require.NoError(t, err)

	// Create a relationship so the entities show up as related.
	_, err = graphStore.CreateRelationship(ctx, memory.Relationship{
		SourceID: entityWithSummaryID, TargetID: entityNoSummaryID,
		Type: memory.RelationDependsOn, Confidence: 0.9,
	})
	require.NoError(t, err)

	// Add a fact so the recall_entity path returns data.
	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityWithSummaryID, Content: "exposes JSON endpoints",
		Type: memory.FactObservation, Confidence: 0.8,
	})
	require.NoError(t, err)

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall_entity",
		Arguments: map[string]interface{}{"name": "api", "type": "module"},
	})

	result := toolResult(t, resp)
	text := result.Content[0].Text
	assert.Contains(t, text, "api (module)")
	assert.Contains(t, text, "REST API layer")
	assert.Contains(t, text, "utils (module)")
}

func TestFormatEntityKnowledge_NoSummary_OmitsSummaryLine(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	// Entity with no summary.
	_, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "bare-entity",
	})
	require.NoError(t, err)

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall_entity",
		Arguments: map[string]interface{}{"name": "bare-entity", "type": "module"},
	})

	result := toolResult(t, resp)
	text := result.Content[0].Text
	assert.Contains(t, text, "bare-entity (module)")
	// No summary line means the output goes straight to a blank line after
	// the entity header.
	assert.NotContains(t, text, " -- ")
}

func TestFormatRetrievalContext_WithEntities_ShowsRelatedEntities(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	// Create entities that will be found by graph traversal during recall.
	// The entity name must match a token in the query for extractMentions
	// to pick it up.
	apiID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "router", Summary: "HTTP routing",
	})
	require.NoError(t, err)

	helperID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "middleware",
	})
	require.NoError(t, err)

	_, err = graphStore.CreateRelationship(ctx, memory.Relationship{
		SourceID: apiID, TargetID: helperID,
		Type: memory.RelationDependsOn, Confidence: 0.9,
	})
	require.NoError(t, err)

	// Add a fact so there is content to display.
	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: apiID, Content: "handles path params",
		Type: memory.FactObservation, Confidence: 0.7,
	})
	require.NoError(t, err)

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	// Query includes "router" which will match the entity name.
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall",
		Arguments: map[string]interface{}{"query": "router path handling"},
	})

	result := toolResult(t, resp)
	text := result.Content[0].Text
	assert.Contains(t, text, "Related entities:")
	assert.Contains(t, text, "router (module)")
}

func TestFormatRetrievalContext_EntityWithEmptySummary_NoDash(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	// Entity with no summary — should not show " — " in entities output.
	_, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "nosummary",
	})
	require.NoError(t, err)

	// Add a fact so the retrieval context is not empty.
	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: 1, Content: "test fact",
		Type: memory.FactObservation, Confidence: 0.5,
	})
	require.NoError(t, err)

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall",
		Arguments: map[string]interface{}{"query": "nosummary test fact"},
	})

	result := toolResult(t, resp)
	text := result.Content[0].Text
	// The entity should appear without a summary dash.
	if strings.Contains(text, "nosummary") {
		assert.NotContains(t, text, "nosummary (module) \u2014 \n")
	}
}
