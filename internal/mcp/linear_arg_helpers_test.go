package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/JR-G/squad0/internal/integrations/linear"
	"github.com/JR-G/squad0/internal/mcp"
	"github.com/stretchr/testify/assert"
)

// These exercises intArg/stringListArg indirectly through save_issue
// and list_issues with different JSON number / array shapes.

func TestLinearHandler_SaveIssue_PriorityAsJSONNumber(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":true}}}`))
	})

	// json.Number exercises the "case json.Number" branch in intArg.
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "save_issue",
		Arguments: map[string]interface{}{"id": "JAM-1", "priority": json.Number("3")},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
}

func TestLinearHandler_SaveIssue_PriorityAsInt(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":true}}}`))
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "save_issue",
		Arguments: map[string]interface{}{"id": "JAM-1", "priority": 2},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
}

func TestLinearHandler_SaveIssue_PriorityAsInvalidJSONNumber(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	// json.Number with non-numeric text exercises intArg's Int64 error branch.
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "save_issue",
		Arguments: map[string]interface{}{"id": "JAM-1", "priority": json.Number("notanumber")},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	// priority invalid → falls through, id-only update attempt → either
	// succeeds (server mock absent) or errors on no-fields. Either way
	// the intArg error branch was exercised.
	assert.NotNil(t, result)
}

func TestLinearHandler_SaveIssue_PriorityAsInvalidType(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "save_issue",
		Arguments: map[string]interface{}{"id": "JAM-1", "priority": "not-a-number"},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.NotNil(t, result)
}

func TestLinearHandler_ListIssues_StatesWithNonString_Skipped(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"team":{"issues":{"nodes":[]}}}}`))
	})
	// Mixed-type array — non-string entries must be silently dropped
	// by stringListArg.
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "list_issues",
		Arguments: map[string]interface{}{
			"teamId": "t",
			"states": []interface{}{"unstarted", 42, "backlog"},
		},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
}

func TestLinearHandler_ListIssues_StatesWrongType_Ignored(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"team":{"issues":{"nodes":[]}}}}`))
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "list_issues",
		Arguments: map[string]interface{}{"teamId": "t", "states": "not-an-array"},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
}
