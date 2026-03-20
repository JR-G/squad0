package mcp_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/mcp"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *memory.DB {
	t.Helper()
	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestHandler(t *testing.T) *mcp.MemoryHandler {
	t.Helper()
	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	embedder := &fakeTestEmbedder{vector: []float32{0.1, 0.2, 0.3}}
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	return mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)
}

type fakeTestEmbedder struct {
	vector []float32
}

func (emb *fakeTestEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return emb.vector, nil
}

func toolResult(t *testing.T, resp mcp.JSONRPCResponse) mcp.ToolResult {
	t.Helper()
	result, ok := resp.Result.(mcp.ToolResult)
	require.True(t, ok, "expected ToolResult, got %T", resp.Result)
	return result
}

func TestMemoryHandler_HandleInit_ReturnsCapabilities(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	resp := handler.HandleInitialize(1) //nolint:misspell // MCP protocol method name
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Nil(t, resp.Error)
}

func TestMemoryHandler_HandleToolsList_ReturnsAllTools(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	resp := handler.HandleToolsList(1)
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)
	tools, ok := result["tools"].([]mcp.ToolDefinition)
	require.True(t, ok)
	assert.Len(t, tools, 5)
}

func TestMemoryHandler_RememberFact_StoresAndRecalls(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)

	storeResp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "remember_fact",
		Arguments: map[string]interface{}{"entity": "payments", "content": "Stripe needs idempotency keys", "type": "warning"},
	})
	result := toolResult(t, storeResp)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Remembered")

	recallResp := handler.HandleToolsCall(2, mcp.ToolCallParams{
		Name:      "recall_entity",
		Arguments: map[string]interface{}{"name": "payments"},
	})
	recallResult := toolResult(t, recallResp)
	assert.Contains(t, recallResult.Content[0].Text, "idempotency")
}

func TestMemoryHandler_StoreBelief(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "store_belief",
		Arguments: map[string]interface{}{"content": "explicit error handling is worth it"},
	})
	result := toolResult(t, resp)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Belief stored")
}

func TestMemoryHandler_NoteEntity_CreatesNew(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "note_entity",
		Arguments: map[string]interface{}{"name": "auth", "type": "module", "summary": "JWT auth"},
	})
	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "Created")
}

func TestMemoryHandler_NoteEntity_UpdatesExisting(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "note_entity",
		Arguments: map[string]interface{}{"name": "auth", "summary": "old"},
	})
	resp := handler.HandleToolsCall(2, mcp.ToolCallParams{
		Name:      "note_entity",
		Arguments: map[string]interface{}{"name": "auth", "summary": "updated"},
	})
	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "Updated")
}

func TestMemoryHandler_RecallEntity_Unknown_ReturnsNoKnowledge(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall_entity",
		Arguments: map[string]interface{}{"name": "nonexistent"},
	})
	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "No knowledge")
}

func TestMemoryHandler_Recall_EmptyDB(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "recall",
		Arguments: map[string]interface{}{"query": "anything"},
	})
	result := toolResult(t, resp)
	assert.Contains(t, result.Content[0].Text, "No relevant memories")
}

func TestMemoryHandler_MissingRequiredArgs_ReturnsError(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	tests := []struct {
		name string
		tool string
		args map[string]interface{}
	}{
		{"recall without query", "recall", map[string]interface{}{}},
		{"remember_fact without entity", "remember_fact", map[string]interface{}{"content": "x"}},
		{"remember_fact without content", "remember_fact", map[string]interface{}{"entity": "x"}},
		{"store_belief without content", "store_belief", map[string]interface{}{}},
		{"note_entity without name", "note_entity", map[string]interface{}{}},
		{"recall_entity without name", "recall_entity", map[string]interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: tt.tool, Arguments: tt.args})
			result := toolResult(t, resp)
			assert.True(t, result.IsError)
		})
	}
}

func TestMemoryHandler_UnknownTool_ReturnsError(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "nonexistent", Arguments: map[string]interface{}{}})
	result := toolResult(t, resp)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "unknown tool")
}
