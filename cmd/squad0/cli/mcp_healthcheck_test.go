package cli

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMCPInit_ValidInit_ReturnsServers(t *testing.T) {
	t.Parallel()

	raw := `{"type":"system","subtype":"init","mcp_servers":[{"name":"claude.ai Linear","status":"connected"},{"name":"memory","status":"connected"}]}
{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`

	init, ok := parseMCPInit(raw)
	assert.True(t, ok)
	assert.Len(t, init.MCPServers, 2)
	assert.Equal(t, "claude.ai Linear", init.MCPServers[0].Name)
	assert.Equal(t, "connected", init.MCPServers[0].Status)
}

func TestParseMCPInit_NoInitLine_ReturnsFalse(t *testing.T) {
	t.Parallel()

	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"no init"}]}}`
	_, ok := parseMCPInit(raw)
	assert.False(t, ok)
}

func TestParseMCPInit_EmptyInput_ReturnsFalse(t *testing.T) {
	t.Parallel()

	_, ok := parseMCPInit("")
	assert.False(t, ok)
}

func TestParseMCPInit_MalformedJSON_ReturnsFalse(t *testing.T) {
	t.Parallel()

	_, ok := parseMCPInit(`{not json at all`)
	assert.False(t, ok)
}

func TestFindServer_Found(t *testing.T) {
	t.Parallel()

	init := mcpInitMessage{
		MCPServers: []mcpServerStatus{
			{Name: "memory", Status: "connected"},
			{Name: "claude.ai Linear", Status: "needs-auth"},
		},
	}

	server := findServer(init, "claude.ai Linear")
	assert.NotNil(t, server)
	assert.Equal(t, "needs-auth", server.Status)
}

func TestFindServer_NotFound(t *testing.T) {
	t.Parallel()

	init := mcpInitMessage{
		MCPServers: []mcpServerStatus{
			{Name: "memory", Status: "connected"},
		},
	}

	assert.Nil(t, findServer(init, "claude.ai Linear"))
}

func TestFindServer_EmptyServers(t *testing.T) {
	t.Parallel()

	init := mcpInitMessage{}
	assert.Nil(t, findServer(init, "anything"))
}

func TestMCPSetupHint_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()

	hint := mcpSetupHint()
	assert.Contains(t, hint, "Linear MCP")
	assert.Contains(t, hint, "squad0-memory-mcp")
}

func TestAssertLinearHealthy_NotAdvertised_ReturnsError(t *testing.T) {
	t.Parallel()

	err := assertLinearHealthy(mcpInitMessage{})

	assert.ErrorContains(t, err, "not advertised")
}

func TestAssertLinearHealthy_NotConnected_ReturnsStatusError(t *testing.T) {
	t.Parallel()

	init := mcpInitMessage{
		MCPServers: []mcpServerStatus{{Name: "claude.ai Linear", Status: "failed"}},
	}

	err := assertLinearHealthy(init)

	assert.ErrorContains(t, err, "status=\"failed\"")
}

func TestAssertLinearHealthy_ConnectedNoTools_ReturnsToolError(t *testing.T) {
	t.Parallel()

	init := mcpInitMessage{
		MCPServers: []mcpServerStatus{{Name: "claude.ai Linear", Status: "connected"}},
		Tools:      []string{"Bash", "Read", "Grep"},
	}

	err := assertLinearHealthy(init)

	assert.ErrorContains(t, err, "no mcp__claude_ai_Linear__")
}

func TestAssertLinearHealthy_ConnectedWithTools_ReturnsNil(t *testing.T) {
	t.Parallel()

	init := mcpInitMessage{
		MCPServers: []mcpServerStatus{{Name: "claude.ai Linear", Status: "connected"}},
		Tools:      []string{"Bash", "mcp__claude_ai_Linear__list_issues"},
	}

	assert.NoError(t, assertLinearHealthy(init))
}

func TestAssertMemoryHealthy_NotAdvertised_ReturnsError(t *testing.T) {
	t.Parallel()

	err := assertMemoryHealthy(mcpInitMessage{})

	assert.ErrorContains(t, err, "not advertised")
}

func TestAssertMemoryHealthy_UserScopeConnected_ReturnsNil(t *testing.T) {
	t.Parallel()

	init := mcpInitMessage{
		MCPServers: []mcpServerStatus{{Name: "squad0-memory", Status: "connected"}},
	}

	assert.NoError(t, assertMemoryHealthy(init))
}

func TestAssertMemoryHealthy_LegacyNameConnected_ReturnsNil(t *testing.T) {
	t.Parallel()

	init := mcpInitMessage{
		MCPServers: []mcpServerStatus{{Name: "memory", Status: "connected"}},
	}

	assert.NoError(t, assertMemoryHealthy(init))
}

func TestAssertMemoryHealthy_FailedStatus_ReturnsErrorWithName(t *testing.T) {
	t.Parallel()

	init := mcpInitMessage{
		MCPServers: []mcpServerStatus{{Name: "squad0-memory", Status: "failed"}},
	}

	err := assertMemoryHealthy(init)

	assert.ErrorContains(t, err, "squad0-memory")
	assert.ErrorContains(t, err, "status=\"failed\"")
}

func TestAssertLinearToolInvoked_ToolUsePresent_ReturnsNil(t *testing.T) {
	t.Parallel()

	raw := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__claude_ai_Linear__list_teams"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","is_error":false}]}}`

	assert.NoError(t, assertLinearToolInvoked(raw))
}

func TestAssertLinearToolInvoked_NoLinearToolUse_ReturnsError(t *testing.T) {
	t.Parallel()

	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}`

	err := assertLinearToolInvoked(raw)
	assert.ErrorContains(t, err, "no mcp__claude_ai_Linear__")
}

func TestAssertLinearToolInvoked_ToolResultError_ReturnsError(t *testing.T) {
	t.Parallel()

	raw := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__claude_ai_Linear__list_teams"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","is_error":true}]}}`

	err := assertLinearToolInvoked(raw)
	assert.ErrorContains(t, err, "returned an error")
}

func TestAssertLinearToolInvoked_MalformedJSONLines_Skipped(t *testing.T) {
	t.Parallel()

	raw := `not json
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__claude_ai_Linear__list_teams"}]}}

{"type":"user","message":{"content":[{"type":"tool_result","is_error":false}]}}`

	assert.NoError(t, assertLinearToolInvoked(raw))
}

func TestAssertLinearToolInvoked_TopLevelContent_AlsoInspected(t *testing.T) {
	t.Parallel()

	// Some stream-json variants put content at the top level rather
	// than nested under "message". The scanner must cover both.
	raw := `{"type":"assistant","content":[{"type":"tool_use","name":"mcp__claude_ai_Linear__list_teams"}]}`

	assert.NoError(t, assertLinearToolInvoked(raw))
}

func TestRealVerifyMCPHealth_NilAgent_ReturnsError(t *testing.T) {
	t.Parallel()

	result := realVerifyMCPHealth(context.Background(), nil, "claude-sonnet-4-6", "")

	require.Error(t, result.OverallErr)
	assert.ErrorContains(t, result.OverallErr, "no PM agent")
}

func TestRealVerifyMCPHealth_EmptyModel_ReturnsError(t *testing.T) {
	t.Parallel()

	pmAgent := agent.NewAgent(agent.RolePM, "model", nil, nil, nil, nil, nil, nil)

	result := realVerifyMCPHealth(context.Background(), pmAgent, "", "")

	require.Error(t, result.OverallErr)
	assert.ErrorContains(t, result.OverallErr, "model is empty")
}

func TestMCPHealthResult_HasIssues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		result MCPHealthResult
		want   bool
	}{
		{"all clear", MCPHealthResult{}, false},
		{"linear err only", MCPHealthResult{LinearErr: assert.AnError}, true},
		{"memory err only", MCPHealthResult{MemoryErr: assert.AnError}, true},
		{"overall err only", MCPHealthResult{OverallErr: assert.AnError}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.result.HasIssues())
		})
	}
}
