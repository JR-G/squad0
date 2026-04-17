package mcp_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JR-G/squad0/internal/integrations/linear"
	"github.com/JR-G/squad0/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLinearHandler(t *testing.T, handler http.HandlerFunc) *mcp.LinearHandler {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := linear.NewClient("test-key").WithAPIURL(server.URL)
	return mcp.NewLinearHandler(client)
}

func TestLinearHandler_HandleInitialize_ReturnsServerInfo(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleInitialize(1)
	assert.Equal(t, "2.0", resp.JSONRPC)
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)
	info, _ := result["serverInfo"].(map[string]interface{})
	assert.Equal(t, "squad0-linear", info["name"])
}

func TestLinearHandler_HandleToolsList_ReturnsAllTools(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleToolsList(2)
	result, _ := resp.Result.(map[string]interface{})
	tools, _ := result["tools"].([]mcp.ToolDefinition)
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"get_issue", "list_issues", "list_teams", "list_issue_statuses", "save_issue", "save_comment"} {
		assert.True(t, names[want], "expected tool %q", want)
	}
}

func TestLinearHandler_HandleToolsCall_UnknownTool_Errors(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleToolsCall(3, mcp.ToolCallParams{Name: "nope"})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "unknown tool")
}

func TestLinearHandler_GetIssue_Success(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"issue":{"id":"u","identifier":"JAM-1","title":"t","state":{"name":"Todo"},"team":{"id":"t"},"labels":{"nodes":[]}}}}`))
	})

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name:      "get_issue",
		Arguments: map[string]interface{}{"id": "JAM-1"},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "JAM-1")
}

func TestLinearHandler_GetIssue_MissingID_Errors(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "get_issue", Arguments: map[string]interface{}{}})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "missing required argument: id")
}

func TestLinearHandler_GetIssue_ClientError_PropagatesMessage(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauth", http.StatusUnauthorized)
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "get_issue", Arguments: map[string]interface{}{"id": "JAM-1"}})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
}

func TestLinearHandler_ListIssues_WithStatesAndLimit(t *testing.T) {
	t.Parallel()

	var captured string
	handler := newLinearHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		_, _ = w.Write([]byte(`{"data":{"team":{"issues":{"nodes":[]}}}}`))
	})

	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "list_issues",
		Arguments: map[string]interface{}{
			"teamId": "team-1",
			"states": []interface{}{"unstarted", "backlog"},
			"limit":  float64(25),
		},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
	assert.Contains(t, captured, "unstarted")
	assert.Contains(t, captured, "first:25")
}

func TestLinearHandler_ListIssues_MissingTeamID_Errors(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "list_issues", Arguments: map[string]interface{}{}})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
}

func TestLinearHandler_ListTeams_ClientError_Propagated(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "list_teams"})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
}

func TestLinearHandler_ListTeams_Success(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"teams":{"nodes":[{"id":"t1","key":"JAM"}]}}}`))
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "list_teams"})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "JAM")
}

func TestLinearHandler_ListIssueStatuses_Success(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"team":{"states":{"nodes":[{"id":"s1","name":"Done"}]}}}}`))
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "list_issue_statuses", Arguments: map[string]interface{}{"teamId": "t1"}})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Done")
}

func TestLinearHandler_ListIssueStatuses_MissingTeamID_Errors(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "list_issue_statuses"})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
}

func TestLinearHandler_SaveIssue_Success(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":true}}}`))
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "save_issue",
		Arguments: map[string]interface{}{
			"id":       "JAM-1",
			"stateId":  "s-done",
			"title":    "new title",
			"priority": float64(2),
		},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Updated JAM-1")
}

func TestLinearHandler_SaveIssue_MissingID_Errors(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "save_issue"})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
}

func TestLinearHandler_SaveIssue_APIError_Propagated(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"no permission"}]}`))
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "save_issue", Arguments: map[string]interface{}{"id": "JAM-1", "stateId": "s"},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "no permission")
}

func TestLinearHandler_SaveIssue_DescriptionOnly_Works(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":true}}}`))
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "save_issue", Arguments: map[string]interface{}{"id": "JAM-1", "description": "new desc"},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
}

func TestLinearHandler_SaveComment_Success(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"commentCreate":{"success":true,"comment":{"id":"c1"}}}}`))
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "save_comment", Arguments: map[string]interface{}{"issueId": "JAM-1", "body": "looks good"},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "Commented on")
}

func TestLinearHandler_SaveComment_MissingIssueID_Errors(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "save_comment", Arguments: map[string]interface{}{"body": "hi"}})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "issueId")
}

func TestLinearHandler_SaveComment_MissingBody_Errors(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{Name: "save_comment", Arguments: map[string]interface{}{"issueId": "JAM-1"}})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "body")
}

func TestLinearHandler_SaveComment_APIError_Propagated(t *testing.T) {
	t.Parallel()
	handler := newLinearHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"bad issue"}]}`))
	})
	resp := handler.HandleToolsCall(1, mcp.ToolCallParams{
		Name: "save_comment", Arguments: map[string]interface{}{"issueId": "JAM-X", "body": "x"},
	})
	result, _ := resp.Result.(mcp.ToolResult)
	assert.True(t, result.IsError)
}

func TestLinearHandler_FullMCPProtocol_ServesToolsList(t *testing.T) {
	t.Parallel()
	handler := mcp.NewLinearHandler(linear.NewClient("k"))
	//nolint:misspell // MCP protocol uses American spelling
	initLine := `{"jsonrpc":"2.0","id":1,"method":"initialize"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	server := mcp.NewServerWithIO(handler, stringReader(initLine), &noopWriter{})
	require.NoError(t, server.Run(context.Background()))
}

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

func stringReader(content string) io.Reader {
	return &stringReadHelper{content: content}
}

type stringReadHelper struct {
	content string
	pos     int
}

func (reader *stringReadHelper) Read(p []byte) (int, error) {
	if reader.pos >= len(reader.content) {
		return 0, io.EOF
	}
	n := copy(p, reader.content[reader.pos:])
	reader.pos += n
	return n, nil
}
