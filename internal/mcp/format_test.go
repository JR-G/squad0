package mcp_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/mcp"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
)

func TestFormatRetrievalContext_EmptyContext_ReturnsNoMemories(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall",
		Arguments: map[string]interface{}{"query": "nonexistent topic"},
	})

	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "No relevant memories")
}

func TestFormatRetrievalContext_WithFacts_ShowsFacts(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := testContext()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "auth"})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "uses JWT tokens", Type: memory.FactObservation, Confidence: 0.8,
	})

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall",
		Arguments: map[string]interface{}{"query": "auth JWT tokens"},
	})

	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "JWT tokens")
}

func TestFormatEntityKnowledge_WithFactsAndRelated(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := testContext()

	authID, _ := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "auth", Summary: "authentication module",
	})
	dbID, _ := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "database",
	})
	_, _ = graphStore.CreateRelationship(ctx, memory.Relationship{
		SourceID: authID, TargetID: dbID, Type: memory.RelationDependsOn, Confidence: 0.9,
	})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: authID, Content: "session tokens expire after 1 hour",
		Type: memory.FactObservation, Confidence: 0.7,
	})

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall_entity",
		Arguments: map[string]interface{}{"name": "auth", "type": "module"},
	})

	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "auth")
	assert.Contains(t, result.Content[0].Text, "session tokens")
	assert.Contains(t, result.Content[0].Text, "database")
}

func testContext() context.Context {
	return context.Background()
}
