package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
