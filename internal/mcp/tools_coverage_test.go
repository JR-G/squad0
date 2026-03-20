package mcp_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/mcp"
	"github.com/stretchr/testify/assert"
)

func TestMemoryHandler_RememberFact_DefaultTypeAndEntityType(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	// Call without "type" or "entity_type" to exercise stringArgOr defaults.
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "remember_fact",
		Arguments: map[string]interface{}{
			"entity":  "router",
			"content": "handles path parameters",
		},
	})

	result := toolResult(t, resp)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Remembered")
	assert.Contains(t, result.Content[0].Text, "router")
}

func TestMemoryHandler_NoteEntity_DefaultType(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	// Call without "type" to exercise stringArgOr default for entity type.
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "note_entity",
		Arguments: map[string]interface{}{
			"name": "scheduler",
		},
	})

	result := toolResult(t, resp)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Created")
	assert.Contains(t, result.Content[0].Text, "module")
}

func TestMemoryHandler_NoteEntity_DefaultSummary(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	// Call without "summary" to exercise stringArgOr default for summary.
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "note_entity",
		Arguments: map[string]interface{}{
			"name": "cache-layer",
			"type": "module",
		},
	})

	result := toolResult(t, resp)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Created")
}

func TestMemoryHandler_RecallEntity_DefaultType(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	// Create an entity first, then recall without specifying type.
	handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "note_entity",
		Arguments: map[string]interface{}{"name": "middleware"},
	})

	resp := handler.HandleToolsCall(2, mcp.ToolCallParams{
		Name: "recall_entity",
		Arguments: map[string]interface{}{
			"name": "middleware",
		},
	})

	result := toolResult(t, resp)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "middleware")
}

func TestStringArg_NonStringValue_ReturnsFalse(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	// Pass a non-string value for "query" — should trigger missing arg error.
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "recall",
		Arguments: map[string]interface{}{
			"query": 42,
		},
	})

	result := toolResult(t, resp)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "missing required argument")
}

func TestStringArgOr_NonStringValue_ReturnsDefault(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	// Pass a non-string value for "type" — should use the default "observation".
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "remember_fact",
		Arguments: map[string]interface{}{
			"entity":  "test-mod",
			"content": "some fact",
			"type":    123,
		},
	})

	result := toolResult(t, resp)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Remembered")
}

func TestMemoryHandler_RecallEntity_FactsByEntity_WithMultipleFacts(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	// Create entity with multiple facts, then recall.
	handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "note_entity",
		Arguments: map[string]interface{}{
			"name":    "database",
			"type":    "module",
			"summary": "persistence layer",
		},
	})
	handler.HandleToolsCall(2, mcp.ToolCallParams{
		Name: "remember_fact",
		Arguments: map[string]interface{}{
			"entity":      "database",
			"entity_type": "module",
			"content":     "uses WAL mode",
			"type":        "observation",
		},
	})
	handler.HandleToolsCall(3, mcp.ToolCallParams{
		Name: "remember_fact",
		Arguments: map[string]interface{}{
			"entity":      "database",
			"entity_type": "module",
			"content":     "fragile migration system",
			"type":        "warning",
		},
	})

	resp := handler.HandleToolsCall(4, mcp.ToolCallParams{
		Name:      "recall_entity",
		Arguments: map[string]interface{}{"name": "database", "type": "module"},
	})

	result := toolResult(t, resp)
	assert.False(t, result.IsError)
	text := result.Content[0].Text
	assert.Contains(t, text, "uses WAL mode")
	assert.Contains(t, text, "fragile migration system")
	assert.Contains(t, text, "Known facts:")
}
