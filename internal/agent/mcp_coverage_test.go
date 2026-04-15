package agent_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMCPConfig_WithBothPaths_IncludesMemoryServer(t *testing.T) {
	t.Parallel()

	cfg := agent.BuildMCPConfig(agent.MCPOptions{
		MemoryBinaryPath: "/usr/local/bin/squad0-memory-mcp",
		AgentDBPath:      "/data/agents/engineer-1.db",
	})

	linear, ok := cfg.MCPServers["linear"]
	require.True(t, ok, "linear server should be present")
	assert.Equal(t, "http", linear.Type)
	assert.Equal(t, "https://mcp.linear.app/mcp", linear.URL)

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

func TestBuildMCPConfig_BothEmpty_OnlyLinear(t *testing.T) {
	t.Parallel()

	cfg := agent.BuildMCPConfig(agent.MCPOptions{})

	assert.Len(t, cfg.MCPServers, 1)
	_, ok := cfg.MCPServers["linear"]
	assert.True(t, ok)
}
