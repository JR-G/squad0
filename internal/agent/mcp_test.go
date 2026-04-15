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

func TestDefaultMCPConfig_OmitsLinear(t *testing.T) {
	t.Parallel()

	// Linear is intentionally absent — Claude Code provides the
	// built-in "claude.ai Linear" managed MCP via the user's main
	// OAuth token. Writing a Linear entry here makes every spawned
	// session crash trying to OAuth the raw Linear MCP URL.
	cfg := agent.DefaultMCPConfig()
	_, ok := cfg.MCPServers["linear"]
	assert.False(t, ok, "linear must NOT be in the generated .mcp.json")
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
	cfg := agent.BuildMCPConfig(agent.MCPOptions{
		MemoryBinaryPath: "/usr/local/bin/squad0-memory-mcp",
		AgentDBPath:      "/data/agents/engineer-1.db",
	})

	err := agent.WriteMCPConfig(dir, cfg)
	require.NoError(t, err)

	path := filepath.Join(dir, ".mcp.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var parsed agent.MCPConfig
	require.NoError(t, json.Unmarshal(data, &parsed))

	memory, ok := parsed.MCPServers["memory"]
	require.True(t, ok, "memory server should be present")
	assert.Equal(t, "/usr/local/bin/squad0-memory-mcp", memory.Command)
	assert.Contains(t, memory.Args, "/data/agents/engineer-1.db")
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
