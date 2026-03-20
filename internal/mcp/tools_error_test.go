package mcp_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/mcp"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newClosedDBHandler creates a MemoryHandler backed by a closed database,
// causing all DB operations to fail.
func newClosedDBHandler(t *testing.T) *mcp.MemoryHandler {
	t.Helper()
	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	require.NoError(t, db.Close())
	return handler
}

func TestMemoryHandler_Recall_DBError_ReturnsError(t *testing.T) {
	t.Parallel()

	handler := newClosedDBHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall",
		Arguments: map[string]interface{}{"query": "anything"},
	})

	result := toolResult(t, resp)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "recall failed")
}

func TestMemoryHandler_RememberFact_EntityError_ReturnsError(t *testing.T) {
	t.Parallel()

	handler := newClosedDBHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "remember_fact",
		Arguments: map[string]interface{}{
			"entity":  "payments",
			"content": "needs retry",
		},
	})

	result := toolResult(t, resp)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "entity error")
}

func TestMemoryHandler_StoreBelief_DBError_ReturnsError(t *testing.T) {
	t.Parallel()

	handler := newClosedDBHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "store_belief",
		Arguments: map[string]interface{}{"content": "this will fail"},
	})

	result := toolResult(t, resp)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "storing belief")
}

func TestMemoryHandler_NoteEntity_DBError_ReturnsError(t *testing.T) {
	t.Parallel()

	handler := newClosedDBHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "note_entity",
		Arguments: map[string]interface{}{
			"name": "will-fail",
			"type": "module",
		},
	})

	result := toolResult(t, resp)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "entity error")
}

func TestMemoryHandler_RecallEntity_ClosedDB_ReturnsNoKnowledge(t *testing.T) {
	t.Parallel()

	handler := newClosedDBHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall_entity",
		Arguments: map[string]interface{}{"name": "anything", "type": "module"},
	})

	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "No knowledge")
}

func TestMemoryHandler_RecallEntity_FactsTableDropped_ReturnsError(t *testing.T) {
	t.Parallel()

	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	_, err = graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "target",
	})
	require.NoError(t, err)

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	// Drop the facts table to make FactsByEntity fail while
	// FindEntityByName still succeeds.
	_, err = db.RawDB().ExecContext(ctx, "DROP TABLE facts")
	require.NoError(t, err)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall_entity",
		Arguments: map[string]interface{}{"name": "target", "type": "module"},
	})

	result := toolResult(t, resp)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "loading facts")
}

func TestMemoryHandler_RecallEntity_RelationshipsTableDropped_ReturnsError(t *testing.T) {
	t.Parallel()

	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	_, err = graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "rel-target",
	})
	require.NoError(t, err)

	// Add a fact so FactsByEntity succeeds.
	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: 1, Content: "a valid fact",
		Type: memory.FactObservation, Confidence: 0.5,
	})
	require.NoError(t, err)

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	// Drop relationships table to make RelatedEntities fail while
	// FindEntityByName and FactsByEntity still succeed.
	_, err = db.RawDB().ExecContext(ctx, "DROP TABLE relationships")
	require.NoError(t, err)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall_entity",
		Arguments: map[string]interface{}{"name": "rel-target", "type": "module"},
	})

	result := toolResult(t, resp)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "loading relationships")
}

func TestMemoryHandler_RememberFact_CreateFactError_ReturnsError(t *testing.T) {
	t.Parallel()

	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	// Pre-create the entity so FindOrCreateEntity succeeds.
	_, err = graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "preexisting",
	})
	require.NoError(t, err)

	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)

	// Drop facts table so CreateFact fails while FindOrCreateEntity succeeds.
	_, err = db.RawDB().ExecContext(ctx, "DROP TABLE facts")
	require.NoError(t, err)

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "remember_fact",
		Arguments: map[string]interface{}{
			"entity":  "preexisting",
			"content": "should fail on fact creation",
		},
	})

	result := toolResult(t, resp)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "storing fact")
}
