package mcp

import (
	"context"
	"fmt"

	"github.com/JR-G/squad0/internal/memory"
)

// MemoryHandler implements RequestHandler with memory-backed tools.
type MemoryHandler struct {
	graphStore   *memory.GraphStore
	factStore    *memory.FactStore
	episodeStore *memory.EpisodeStore
	retriever    *memory.Retriever
}

// NewMemoryHandler creates a MemoryHandler with the given stores.
func NewMemoryHandler(
	graphStore *memory.GraphStore,
	factStore *memory.FactStore,
	episodeStore *memory.EpisodeStore,
	retriever *memory.Retriever,
) *MemoryHandler {
	return &MemoryHandler{
		graphStore:   graphStore,
		factStore:    factStore,
		episodeStore: episodeStore,
		retriever:    retriever,
	}
}

// HandleInitialize returns the server capabilities.
func (handler *MemoryHandler) HandleInitialize(id interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "squad0-memory",
				"version": "0.1.0",
			},
		},
	}
}

// HandleToolsList returns all available memory tools.
func (handler *MemoryHandler) HandleToolsList(id interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"tools": memoryTools(),
		},
	}
}

// HandleToolsCall dispatches a tool call to the appropriate handler.
func (handler *MemoryHandler) HandleToolsCall(id interface{}, params ToolCallParams) JSONRPCResponse {
	ctx := context.Background()

	var result ToolResult

	switch params.Name {
	case "recall":
		result = handler.handleRecall(ctx, params.Arguments)
	case "remember_fact":
		result = handler.handleRememberFact(ctx, params.Arguments)
	case "store_belief":
		result = handler.handleStoreBelief(ctx, params.Arguments)
	case "note_entity":
		result = handler.handleNoteEntity(ctx, params.Arguments)
	case "recall_entity":
		result = handler.handleRecallEntity(ctx, params.Arguments)
	default:
		result = toolError(fmt.Sprintf("unknown tool: %s", params.Name))
	}

	return JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func (handler *MemoryHandler) handleRecall(ctx context.Context, args map[string]interface{}) ToolResult {
	query, ok := stringArg(args, "query")
	if !ok {
		return toolError("missing required argument: query")
	}

	memCtx, err := handler.retriever.Retrieve(ctx, query, nil)
	if err != nil {
		return toolError(fmt.Sprintf("recall failed: %v", err))
	}

	return toolText(formatRetrievalContext(memCtx))
}

func (handler *MemoryHandler) handleRememberFact(ctx context.Context, args map[string]interface{}) ToolResult {
	entityName, ok := stringArg(args, "entity")
	if !ok {
		return toolError("missing required argument: entity")
	}

	content, ok := stringArg(args, "content")
	if !ok {
		return toolError("missing required argument: content")
	}

	factType := stringArgOr(args, "type", "observation")
	entityType := stringArgOr(args, "entity_type", "module")

	entity, _, err := handler.graphStore.FindOrCreateEntity(ctx, memory.EntityType(entityType), entityName, "")
	if err != nil {
		return toolError(fmt.Sprintf("entity error: %v", err))
	}

	_, err = handler.factStore.CreateFact(ctx, memory.Fact{
		EntityID:   entity.ID,
		Content:    content,
		Type:       memory.FactType(factType),
		Confidence: 0.5,
	})
	if err != nil {
		return toolError(fmt.Sprintf("storing fact: %v", err))
	}

	return toolText(fmt.Sprintf("Remembered: %s (about %s)", content, entityName))
}

func (handler *MemoryHandler) handleStoreBelief(ctx context.Context, args map[string]interface{}) ToolResult {
	content, ok := stringArg(args, "content")
	if !ok {
		return toolError("missing required argument: content")
	}

	_, err := handler.factStore.CreateBelief(ctx, memory.Belief{
		Content:    content,
		Confidence: 0.5,
	})
	if err != nil {
		return toolError(fmt.Sprintf("storing belief: %v", err))
	}

	return toolText(fmt.Sprintf("Belief stored: %s", content))
}

func (handler *MemoryHandler) handleNoteEntity(ctx context.Context, args map[string]interface{}) ToolResult {
	name, ok := stringArg(args, "name")
	if !ok {
		return toolError("missing required argument: name")
	}

	entityType := stringArgOr(args, "type", "module")
	summary := stringArgOr(args, "summary", "")

	entity, created, err := handler.graphStore.FindOrCreateEntity(ctx, memory.EntityType(entityType), name, summary)
	if err != nil {
		return toolError(fmt.Sprintf("entity error: %v", err))
	}

	if !created && summary != "" {
		_ = handler.graphStore.UpdateEntitySummary(ctx, entity.ID, summary)
	}

	action := "Updated"
	if created {
		action = "Created"
	}

	return toolText(fmt.Sprintf("%s entity: %s (%s)", action, name, entityType))
}

func (handler *MemoryHandler) handleRecallEntity(ctx context.Context, args map[string]interface{}) ToolResult {
	name, ok := stringArg(args, "name")
	if !ok {
		return toolError("missing required argument: name")
	}

	entityType := stringArgOr(args, "type", "module")

	entity, err := handler.graphStore.FindEntityByName(ctx, memory.EntityType(entityType), name)
	if err != nil {
		return toolText(fmt.Sprintf("No knowledge about %s (%s)", name, entityType))
	}

	facts, err := handler.factStore.FactsByEntity(ctx, entity.ID)
	if err != nil {
		return toolError(fmt.Sprintf("loading facts: %v", err))
	}

	related, err := handler.graphStore.RelatedEntities(ctx, entity.ID, 2)
	if err != nil {
		return toolError(fmt.Sprintf("loading relationships: %v", err))
	}

	return toolText(formatEntityKnowledge(entity, facts, related))
}

func memoryTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "recall",
			Description: "Search your memory for knowledge relevant to a topic. Use at the start of every session and when you need context about something.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"query": prop("What to search for — a topic, file name, module, or concept"),
			}, "query"),
		},
		{
			Name:        "remember_fact",
			Description: "Store a specific fact you've learned about a module, file, or concept. Use immediately when you discover something important.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"entity":      prop("The module, file, or concept this fact is about"),
				"content":     prop("The fact to remember"),
				"type":        prop("One of: observation, preference, warning, technique"),
				"entity_type": prop("One of: module, file, pattern, tool, concept"),
			}, "entity", "content"),
		},
		{
			Name:        "store_belief",
			Description: "Store a belief or principle you've developed from experience. Beliefs evolve over time as they're confirmed or contradicted.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"content": prop("The belief or principle"),
			}, "content"),
		},
		{
			Name:        "note_entity",
			Description: "Record that you've encountered a module, file, tool, or concept. Creates it if new, updates the summary if it already exists.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"name":    prop("Entity name"),
				"type":    prop("One of: module, file, pattern, tool, concept"),
				"summary": prop("Brief description"),
			}, "name"),
		},
		{
			Name:        "recall_entity",
			Description: "Get everything you know about a specific entity — its facts, beliefs, and connections.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"name": prop("Entity name to look up"),
				"type": prop("One of: module, file, pattern, tool, concept"),
			}, "name"),
		},
	}
}

func prop(description string) map[string]string {
	return map[string]string{"type": "string", "description": description}
}

func schemaWithRequired(properties map[string]interface{}, required ...string) map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func toolText(text string) ToolResult {
	return ToolResult{Content: []ToolContent{{Type: "text", Text: text}}}
}

func toolError(message string) ToolResult {
	return ToolResult{Content: []ToolContent{{Type: "text", Text: message}}, IsError: true}
}

func stringArg(args map[string]interface{}, key string) (string, bool) {
	val, ok := args[key]
	if !ok {
		return "", false
	}

	str, ok := val.(string)
	return str, ok
}

func stringArgOr(args map[string]interface{}, key, defaultVal string) string {
	val, ok := stringArg(args, key)
	if !ok {
		return defaultVal
	}

	return val
}
