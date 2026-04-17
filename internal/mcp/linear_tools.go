package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/JR-G/squad0/internal/integrations/linear"
)

// LinearHandler implements RequestHandler with Linear GraphQL-backed
// tools. Exists so squad0 can avoid the OAuth-managed claude.ai
// Linear connector, whose tokens go stale without warning and take
// every claude -p subprocess with them.
type LinearHandler struct {
	client *linear.Client
}

// NewLinearHandler returns a LinearHandler backed by the given client.
func NewLinearHandler(client *linear.Client) *LinearHandler {
	return &LinearHandler{client: client}
}

// HandleInitialize returns the server capabilities.
func (handler *LinearHandler) HandleInitialize(id interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo": map[string]interface{}{
				"name":    "squad0-linear",
				"version": "0.1.0",
			},
		},
	}
}

// HandleToolsList returns the Linear tools.
func (handler *LinearHandler) HandleToolsList(id interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  map[string]interface{}{"tools": linearTools()},
	}
}

// HandleToolsCall dispatches a tool call.
func (handler *LinearHandler) HandleToolsCall(id interface{}, params ToolCallParams) JSONRPCResponse {
	ctx := context.Background()

	var result ToolResult

	switch params.Name {
	case "get_issue":
		result = handler.handleGetIssue(ctx, params.Arguments)
	case "list_issues":
		result = handler.handleListIssues(ctx, params.Arguments)
	case "list_teams":
		result = handler.handleListTeams(ctx, params.Arguments)
	case "list_issue_statuses":
		result = handler.handleListIssueStatuses(ctx, params.Arguments)
	case "save_issue":
		result = handler.handleSaveIssue(ctx, params.Arguments)
	case "save_comment":
		result = handler.handleSaveComment(ctx, params.Arguments)
	default:
		result = toolError(fmt.Sprintf("unknown tool: %s", params.Name))
	}

	return JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func (handler *LinearHandler) handleGetIssue(ctx context.Context, args map[string]interface{}) ToolResult {
	id, ok := stringArg(args, "id")
	if !ok {
		return toolError("missing required argument: id")
	}

	issue, err := handler.client.GetIssue(ctx, id)
	if err != nil {
		return toolError(err.Error())
	}
	return jsonResult(issue)
}

func (handler *LinearHandler) handleListIssues(ctx context.Context, args map[string]interface{}) ToolResult {
	teamID, ok := stringArg(args, "teamId")
	if !ok {
		return toolError("missing required argument: teamId")
	}

	filter := linear.ListIssuesFilter{TeamID: teamID}
	if states := stringListArg(args, "states"); len(states) > 0 {
		filter.States = states
	}
	if limit, ok := intArg(args, "limit"); ok {
		filter.Limit = limit
	}

	issues, err := handler.client.ListIssues(ctx, filter)
	if err != nil {
		return toolError(err.Error())
	}
	return jsonResult(map[string]interface{}{"issues": issues})
}

func (handler *LinearHandler) handleListTeams(ctx context.Context, _ map[string]interface{}) ToolResult {
	teams, err := handler.client.ListTeams(ctx)
	if err != nil {
		return toolError(err.Error())
	}
	return jsonResult(map[string]interface{}{"teams": teams})
}

func (handler *LinearHandler) handleListIssueStatuses(ctx context.Context, args map[string]interface{}) ToolResult {
	teamID, ok := stringArg(args, "teamId")
	if !ok {
		return toolError("missing required argument: teamId")
	}

	states, err := handler.client.ListIssueStatuses(ctx, teamID)
	if err != nil {
		return toolError(err.Error())
	}
	return jsonResult(map[string]interface{}{"states": states})
}

func (handler *LinearHandler) handleSaveIssue(ctx context.Context, args map[string]interface{}) ToolResult {
	id, ok := stringArg(args, "id")
	if !ok {
		return toolError("missing required argument: id")
	}

	update := linear.SaveIssueUpdate{}
	if stateID, ok := stringArg(args, "stateId"); ok {
		update.StateID = stateID
	}
	if title, ok := stringArg(args, "title"); ok {
		update.Title = title
	}
	if description, ok := stringArg(args, "description"); ok {
		update.Description = description
	}
	if priority, ok := intArg(args, "priority"); ok {
		update.Priority = &priority
	}

	if err := handler.client.SaveIssue(ctx, id, update); err != nil {
		return toolError(err.Error())
	}
	return toolText(fmt.Sprintf("Updated %s", id))
}

func (handler *LinearHandler) handleSaveComment(ctx context.Context, args map[string]interface{}) ToolResult {
	issueID, ok := stringArg(args, "issueId")
	if !ok {
		return toolError("missing required argument: issueId")
	}
	body, ok := stringArg(args, "body")
	if !ok {
		return toolError("missing required argument: body")
	}

	if err := handler.client.SaveComment(ctx, issueID, body); err != nil {
		return toolError(err.Error())
	}
	return toolText(fmt.Sprintf("Commented on %s", issueID))
}

func linearTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_issue",
			Description: "Fetch a Linear issue by identifier (e.g. \"JAM-12\") or UUID. Returns title, description, state, team, priority, labels, URL.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"id": prop("Issue identifier like \"JAM-12\" or a UUID"),
			}, "id"),
		},
		{
			Name:        "list_issues",
			Description: "List issues in a team, optionally filtered by workflow state type.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"teamId": prop("The team UUID (use list_teams to discover)"),
				"states": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional workflow state types to include, e.g. [\"unstarted\", \"backlog\"]",
				},
				"limit": map[string]interface{}{"type": "integer", "description": "Max issues to return (default 50)"},
			}, "teamId"),
		},
		{
			Name:        "list_teams",
			Description: "List all teams visible to the API key.",
			InputSchema: schemaWithRequired(map[string]interface{}{}),
		},
		{
			Name:        "list_issue_statuses",
			Description: "List workflow states (e.g. Todo, In Progress, Done) for a team.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"teamId": prop("The team UUID"),
			}, "teamId"),
		},
		{
			Name:        "save_issue",
			Description: "Update one or more fields on a Linear issue. At least one of stateId, title, description, priority must be provided.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"id":          prop("Issue identifier or UUID"),
				"stateId":     prop("New workflow state UUID (look up via list_issue_statuses)"),
				"title":       prop("New title"),
				"description": prop("New description"),
				"priority":    map[string]interface{}{"type": "integer", "description": "Priority 0-4 (0=None, 1=Urgent, 2=High, 3=Normal, 4=Low)"},
			}, "id"),
		},
		{
			Name:        "save_comment",
			Description: "Post a comment on a Linear issue.",
			InputSchema: schemaWithRequired(map[string]interface{}{
				"issueId": prop("Issue UUID or identifier"),
				"body":    prop("Comment body (Markdown supported)"),
			}, "issueId", "body"),
		},
	}
}

func intArg(args map[string]interface{}, key string) (int, bool) {
	val, ok := args[key]
	if !ok {
		return 0, false
	}
	switch num := val.(type) {
	case float64:
		return int(num), true
	case int:
		return num, true
	case json.Number:
		asInt, err := num.Int64()
		if err != nil {
			return 0, false
		}
		return int(asInt), true
	}
	return 0, false
}

func stringListArg(args map[string]interface{}, key string) []string {
	val, ok := args[key]
	if !ok {
		return nil
	}
	list, ok := val.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if str, ok := item.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

func jsonResult(payload interface{}) ToolResult {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("marshalling result: %v", err))
	}
	return toolText(string(data))
}
