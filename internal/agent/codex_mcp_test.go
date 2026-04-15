package agent_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mcpRunnerCall struct {
	name string
	args []string
}

type fakeMCPRunner struct {
	calls []mcpRunnerCall
	err   error
}

func (runner *fakeMCPRunner) Run(_ context.Context, _, _, name string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, mcpRunnerCall{name: name, args: args})
	return nil, runner.err
}

func TestEnsureCodexMCPServers_NoMemory_NoCalls(t *testing.T) {
	t.Parallel()

	// Linear is intentionally not in the codex server list (see
	// BuildCodexMCPServers doc). With no memory path set there are
	// no servers to register, so the runner is never called.
	runner := &fakeMCPRunner{}
	servers := agent.BuildCodexMCPServers(agent.MCPOptions{})

	err := agent.EnsureCodexMCPServers(context.Background(), runner, servers)

	require.NoError(t, err)
	assert.Empty(t, runner.calls)
}

func TestEnsureCodexMCPServers_WithMemory_RegistersOnlyMemory(t *testing.T) {
	t.Parallel()

	runner := &fakeMCPRunner{}
	servers := agent.BuildCodexMCPServers(agent.MCPOptions{
		MemoryBinaryPath: "/usr/local/bin/memory-mcp",
		AgentDBPath:      "/data/agents/engineer-1.db",
	})

	err := agent.EnsureCodexMCPServers(context.Background(), runner, servers)

	require.NoError(t, err)
	// 1 server (memory) × 2 calls (remove + add) = 2
	require.Len(t, runner.calls, 2)

	// Memory server add should include the binary and db path.
	addMemoryCall := runner.calls[1]
	joined := strings.Join(addMemoryCall.args, " ")
	assert.Contains(t, joined, "memory")
	assert.Contains(t, joined, "/usr/local/bin/memory-mcp")
	assert.Contains(t, joined, "/data/agents/engineer-1.db")
}

func TestEnsureCodexMCPServers_AddError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := &fakeMCPRunner{}
	// Fail on the second call (the add) for the memory server.
	wrapper := &conditionalFailRunner{base: runner, failOnCall: 1}

	servers := agent.BuildCodexMCPServers(agent.MCPOptions{
		MemoryBinaryPath: "/usr/local/bin/memory-mcp",
		AgentDBPath:      "/data/agents/engineer-1.db",
	})

	err := agent.EnsureCodexMCPServers(context.Background(), wrapper, servers)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "memory")
}

type conditionalFailRunner struct {
	base       *fakeMCPRunner
	failOnCall int
	calls      int
}

func (runner *conditionalFailRunner) Run(ctx context.Context, stdin, workDir, name string, args ...string) ([]byte, error) {
	runner.calls++
	if runner.calls == runner.failOnCall+1 {
		return nil, fmt.Errorf("codex mcp add failed")
	}
	return runner.base.Run(ctx, stdin, workDir, name, args...)
}

func TestBuildCodexMCPServers_NoMemory_Empty(t *testing.T) {
	t.Parallel()

	// No memory + no Linear = empty.
	servers := agent.BuildCodexMCPServers(agent.MCPOptions{})
	assert.Empty(t, servers)
}

func TestBuildCodexMCPServers_WithMemory_OnlyMemory(t *testing.T) {
	t.Parallel()

	servers := agent.BuildCodexMCPServers(agent.MCPOptions{
		MemoryBinaryPath: "/usr/local/bin/memory-mcp",
		AgentDBPath:      "/data/agents/engineer-1.db",
	})

	require.Len(t, servers, 1)
	assert.Equal(t, "memory", servers[0].Name)
}

func TestBuildCodexMCPServers_WithEnv_PassesEnv(t *testing.T) {
	t.Parallel()

	runner := &fakeMCPRunner{}
	servers := []agent.CodexMCPServer{
		{
			Name:    "test",
			Command: "test-cmd",
			Args:    []string{"--flag"},
			Env:     map[string]string{"API_KEY": "secret123"},
		},
	}

	err := agent.EnsureCodexMCPServers(context.Background(), runner, servers)

	require.NoError(t, err)
	addCall := runner.calls[1]
	joined := strings.Join(addCall.args, " ")
	assert.Contains(t, joined, "--env")
	assert.Contains(t, joined, "API_KEY=secret123")
}
