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

func TestEnsureCodexMCPServers_RegistersLinear(t *testing.T) {
	t.Parallel()

	runner := &fakeMCPRunner{}
	servers := agent.BuildCodexMCPServers(agent.MCPOptions{})

	err := agent.EnsureCodexMCPServers(context.Background(), runner, servers)

	require.NoError(t, err)
	require.Len(t, runner.calls, 2) // remove + add

	// First call: remove
	assert.Equal(t, "codex", runner.calls[0].name)
	assert.Contains(t, runner.calls[0].args, "remove")
	assert.Contains(t, runner.calls[0].args, "linear")

	// Second call: add
	assert.Equal(t, "codex", runner.calls[1].name)
	assert.Contains(t, runner.calls[1].args, "add")
	assert.Contains(t, runner.calls[1].args, "linear")
	assert.Contains(t, runner.calls[1].args, "bunx")
}

func TestEnsureCodexMCPServers_WithMemory_RegistersBoth(t *testing.T) {
	t.Parallel()

	runner := &fakeMCPRunner{}
	servers := agent.BuildCodexMCPServers(agent.MCPOptions{
		MemoryBinaryPath: "/usr/local/bin/memory-mcp",
		AgentDBPath:      "/data/agents/engineer-1.db",
	})

	err := agent.EnsureCodexMCPServers(context.Background(), runner, servers)

	require.NoError(t, err)
	// 2 servers × 2 calls each (remove + add) = 4
	require.Len(t, runner.calls, 4)

	// Memory server add should include the binary and db path.
	addMemoryCall := runner.calls[3]
	joined := strings.Join(addMemoryCall.args, " ")
	assert.Contains(t, joined, "memory")
	assert.Contains(t, joined, "/usr/local/bin/memory-mcp")
	assert.Contains(t, joined, "/data/agents/engineer-1.db")
}

func TestEnsureCodexMCPServers_AddError_ReturnsError(t *testing.T) {
	t.Parallel()

	callCount := 0
	runner := &fakeMCPRunner{}
	// Fail on the second call (the add).
	runner.err = nil
	original := runner
	wrapper := &conditionalFailRunner{base: original, failOnCall: 1}

	servers := agent.BuildCodexMCPServers(agent.MCPOptions{})

	err := agent.EnsureCodexMCPServers(context.Background(), wrapper, servers)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "linear")
	_ = callCount
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

func TestBuildCodexMCPServers_NoMemory_OnlyLinear(t *testing.T) {
	t.Parallel()

	servers := agent.BuildCodexMCPServers(agent.MCPOptions{})

	require.Len(t, servers, 1)
	assert.Equal(t, "linear", servers[0].Name)
	assert.Equal(t, "bunx", servers[0].Command)
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
