package agent_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultMCPConfig_HasLinearServer(t *testing.T) {
	t.Parallel()

	cfg := agent.DefaultMCPConfig()

	linear, ok := cfg.MCPServers["linear"]
	require.True(t, ok, "linear server should be present")
	assert.Equal(t, "bunx", linear.Command)
	assert.Contains(t, linear.Args, "@linear/mcp-server")
}

func TestDefaultMCPConfig_NeverUsesNpx(t *testing.T) {
	t.Parallel()

	cfg := agent.DefaultMCPConfig()

	for name, server := range cfg.MCPServers {
		assert.NotEqual(t, "npx", server.Command, "server %s should not use npx", name)
	}
}

func TestWriteMCPConfig_CreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := agent.DefaultMCPConfig()

	err := agent.WriteMCPConfig(dir, cfg)

	require.NoError(t, err)

	path := filepath.Join(dir, ".mcp.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var parsed agent.MCPConfig
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	linear, ok := parsed.MCPServers["linear"]
	require.True(t, ok)
	assert.Equal(t, "bunx", linear.Command)
}

func TestWriteMCPConfig_InvalidDir_ReturnsError(t *testing.T) {
	t.Parallel()

	err := agent.WriteMCPConfig("/nonexistent/dir", agent.DefaultMCPConfig())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing MCP config")
}

func TestRemoveMCPConfig_RemovesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := agent.WriteMCPConfig(dir, agent.DefaultMCPConfig())
	require.NoError(t, err)

	err = agent.RemoveMCPConfig(dir)

	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, ".mcp.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestRemoveMCPConfig_NonexistentFile_ReturnsNil(t *testing.T) {
	t.Parallel()

	err := agent.RemoveMCPConfig(t.TempDir())

	assert.NoError(t, err)
}
