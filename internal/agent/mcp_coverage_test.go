package agent_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMCPConfig_WithBothPaths_IncludesMemoryServerOnly(t *testing.T) {
	t.Parallel()

	cfg := agent.BuildMCPConfig(agent.MCPOptions{
		MemoryBinaryPath: "/usr/local/bin/squad0-memory-mcp",
		AgentDBPath:      "/data/agents/engineer-1.db",
	})

	// Linear is NOT in the .mcp.json — Claude Code provides it via
	// the built-in claude.ai Linear managed MCP.
	_, linearOK := cfg.MCPServers["linear"]
	assert.False(t, linearOK, "linear must NOT be in the generated .mcp.json")

	mem, ok := cfg.MCPServers["memory"]
	require.True(t, ok, "memory server should be present when both paths set")
	assert.Equal(t, "/usr/local/bin/squad0-memory-mcp", mem.Command)
	assert.Equal(t, []string{"--db", "/data/agents/engineer-1.db"}, mem.Args)
}

func TestBuildMCPConfig_OnlyMemoryPath_NoMemoryServer(t *testing.T) {
	t.Parallel()

	cfg := agent.BuildMCPConfig(agent.MCPOptions{
		MemoryBinaryPath: "/usr/local/bin/squad0-memory-mcp",
	})

	_, ok := cfg.MCPServers["memory"]
	assert.False(t, ok, "memory server should not be present without AgentDBPath")
}

func TestBuildMCPConfig_OnlyDBPath_NoMemoryServer(t *testing.T) {
	t.Parallel()

	cfg := agent.BuildMCPConfig(agent.MCPOptions{
		AgentDBPath: "/data/agents/engineer-1.db",
	})

	_, ok := cfg.MCPServers["memory"]
	assert.False(t, ok, "memory server should not be present without MemoryBinaryPath")
}

func TestBuildMCPConfig_BothEmpty_NoServers(t *testing.T) {
	t.Parallel()

	// With no memory binary + DB, there are no servers at all —
	// Claude Code still exposes the claude.ai Linear managed MCP
	// to the spawned subprocess for free.
	cfg := agent.BuildMCPConfig(agent.MCPOptions{})
	assert.Empty(t, cfg.MCPServers)
}
