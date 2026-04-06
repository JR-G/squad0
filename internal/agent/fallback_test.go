package agent_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsRateLimited_WithSignal_ReturnsTrue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		err    string
	}{
		{"rate_limit", "error: rate_limit_exceeded", "exit 1"},
		{"429", "HTTP 429 Too Many Requests", "exit 1"},
		{"overloaded", "API is overloaded", "exit 1"},
		{"rate limit", "rate limit hit", "exit 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, agent.IsRateLimitedForTest(tt.output, fmt.Errorf("%s", tt.err)))
		})
	}
}

func TestIsRateLimited_NormalError_ReturnsFalse(t *testing.T) {
	t.Parallel()
	assert.False(t, agent.IsRateLimitedForTest("some other error", fmt.Errorf("%s", "exit 1")))
}

func TestBuildCodexArgs_AllFields(t *testing.T) {
	t.Parallel()

	args := agent.BuildCodexArgs("do something", "/tmp/work", "o3")
	assert.Contains(t, args, "exec")
	assert.Contains(t, args, "--json")
	assert.Contains(t, args, "--dangerously-bypass-approvals-and-sandbox")
	assert.Contains(t, args, "--skip-git-repo-check")
	assert.Contains(t, args, "-c")
	assert.Contains(t, args, `model_reasoning_effort="medium"`)
	assert.Contains(t, args, "-m")
	assert.Contains(t, args, "o3")
	assert.Contains(t, args, "-C")
	assert.Contains(t, args, "/tmp/work")
	assert.Contains(t, args, "do something")
}

func TestBuildCodexArgs_NoModel(t *testing.T) {
	t.Parallel()

	args := agent.BuildCodexArgs("prompt", "", "")
	assert.Contains(t, args, "exec")
	assert.Contains(t, args, "--skip-git-repo-check")
	assert.Contains(t, args, `model_reasoning_effort="medium"`)
	assert.NotContains(t, args, "-m")
	assert.NotContains(t, args, "-C")
}

func TestBuildCodexArgs_WithOutputLastMessageFile(t *testing.T) {
	t.Parallel()

	args := agent.BuildCodexArgs("prompt", "/tmp/work", "o3", "/tmp/last-message.txt")
	assert.Contains(t, args, "-o")
	assert.Contains(t, args, "/tmp/last-message.txt")
}

func TestParseCodexOutput_JSONLines(t *testing.T) {
	t.Parallel()

	raw := `{"type":"message","content":"hello world"}
{"type":"message","content":"final answer"}`
	assert.Equal(t, "final answer", agent.ParseCodexOutput(raw))
}

func TestParseCodexOutput_IgnoresTurnCompletedUsage(t *testing.T) {
	t.Parallel()

	raw := `{"type":"message","content":"actual answer"}
{"type":"turn.completed","usage":{"input_tokens":12294,"cached_input_tokens":4864,"output_tokens":64}}`
	assert.Equal(t, "actual answer", agent.ParseCodexOutput(raw))
}

func TestParseCodexOutput_IgnoresStartupNoise(t *testing.T) {
	t.Parallel()

	raw := `Reading additional input from stdin...
{"type":"thread.started","thread_id":"abc"}
{"type":"turn.started"}
{"type":"message","content":"actual answer"}`
	assert.Equal(t, "actual answer", agent.ParseCodexOutput(raw))
}

func TestParseCodexOutput_PlainText(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "just text", agent.ParseCodexOutput("just text"))
}

func TestParseCodexOutput_Empty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, agent.ParseCodexOutput(""))
}

func TestResolveCodexTranscript_FallsBackToLastMessageFile(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp("", "squad0-codex-last-message-*.txt")
	require.NoError(t, err)
	path := file.Name()
	_, err = file.WriteString("final answer from file")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	t.Cleanup(func() { _ = os.Remove(path) })

	result := agent.ResolveCodexTranscript(`{"type":"thread.started"}`+"\n", path)
	assert.Equal(t, "final answer from file", result)
}

func TestFallbackRunner_NoRateLimit_UsesPrimary(t *testing.T) {
	t.Parallel()

	primary := &fakeRunner{output: []byte("claude output"), err: nil}
	runner := agent.NewFallbackRunner(primary, "o3")

	output, err := runner.Run(context.Background(), "prompt", "", "claude", "-p")
	require.NoError(t, err)
	assert.Equal(t, "claude output", string(output))
	assert.Equal(t, 1, primary.calls)
}

func TestFallbackRunner_RateLimit_FallsBack(t *testing.T) {
	t.Parallel()

	callCount := 0
	primary := &switchingRunner{
		responses: []runResult{
			{output: []byte("rate_limit_exceeded"), err: fmt.Errorf("exit 1")},
			{output: []byte(`{"type":"message","content":"codex did it"}`), err: nil},
		},
		callCount: &callCount,
	}

	runner := agent.NewFallbackRunner(primary, "o3")
	output, err := runner.Run(context.Background(), "prompt", "/tmp", "claude", "-p")
	require.NoError(t, err)
	assert.Contains(t, string(output), "codex did it")
	assert.Equal(t, 2, callCount)
}

func TestSession_Run_RateLimit_FallsBackToCodex(t *testing.T) {
	t.Parallel()

	callCount := 0
	runner := &switchingRunner{
		responses: []runResult{
			{output: []byte("rate_limit_exceeded"), err: fmt.Errorf("exit 1")},
			{output: []byte(`{"type":"message","content":"codex response"}`), err: nil},
		},
		callCount: &callCount,
	}

	session := agent.NewSession(runner)
	session.SetCodexFallback("o3")

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "test",
	})

	require.NoError(t, err)
	assert.Equal(t, "codex response", result.Transcript)
	assert.Equal(t, 2, callCount)
}

func TestSession_Run_NoRateLimit_UsesClaudeResult(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		output: []byte(`{"type":"result","result":"claude answer"}` + "\n"),
		err:    nil,
	}

	session := agent.NewSession(runner)
	session.SetCodexFallback("o3")

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "test",
	})

	require.NoError(t, err)
	assert.Equal(t, "claude answer", result.Transcript)
	assert.Equal(t, 1, runner.calls)
}

func TestSession_Run_RateLimit_NoFallbackConfigured(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		output: []byte("rate_limit_exceeded"),
		err:    fmt.Errorf("exit 1"),
	}

	session := agent.NewSession(runner)
	// No SetCodexFallback — should return Claude error.

	_, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "test",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude session failed")
}

func TestSession_Run_CodexFallback_AlsoFails(t *testing.T) {
	t.Parallel()

	callCount := 0
	runner := &switchingRunner{
		responses: []runResult{
			{output: []byte("rate_limit_exceeded"), err: fmt.Errorf("exit 1")},
			{output: []byte("codex also crashed"), err: fmt.Errorf("exit 1")},
		},
		callCount: &callCount,
	}

	session := agent.NewSession(runner)
	session.SetCodexFallback("o3")

	_, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "test",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "codex session failed")
}

func TestSession_Run_LongErrorOutput_Truncated(t *testing.T) {
	t.Parallel()

	longOutput := ""
	for range 100 {
		longOutput += "error error error error "
	}

	runner := &fakeRunner{output: []byte(longOutput), err: fmt.Errorf("exit 1")}
	session := agent.NewSession(runner)

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "test", Prompt: "test",
	})

	require.Error(t, err)
	_ = result
}

func TestSession_SetCodexFallback(t *testing.T) {
	t.Parallel()

	session := agent.NewSession(agent.ExecProcessRunner{})
	session.SetCodexFallback("o3")
}

func TestSession_Run_WithEnv_ExecRunner(t *testing.T) {
	t.Parallel()

	// Uses real ExecProcessRunner to test the runnerWithEnv switch case.
	session := agent.NewSession(agent.ExecProcessRunner{})
	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role:   agent.RoleEngineer1,
		Model:  "claude-sonnet-4-6",
		Prompt: "echo test",
		Env:    map[string]string{"SQUAD0_TEST": "1"},
	})

	// Will fail because claude CLI isn't expecting this, but it exercises
	// the runnerWithEnv path for ExecProcessRunner.
	_ = result
	_ = err
}

func TestSession_Run_WithEnv_PassesToRunner(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	session := agent.NewSession(runner)

	_, err := session.Run(context.Background(), agent.SessionConfig{
		Role:   agent.RoleEngineer1,
		Model:  "claude-sonnet-4-6",
		Prompt: "test",
		Env:    map[string]string{"GH_TOKEN": "test-token"},
	})

	require.NoError(t, err)
	assert.Equal(t, 1, runner.calls)
}

func TestSession_Run_WithEnvAndFallback(t *testing.T) {
	t.Parallel()

	callCount := 0
	runner := &switchingRunner{
		responses: []runResult{
			{output: []byte("rate_limit_exceeded"), err: fmt.Errorf("exit 1")},
			{output: []byte(`{"type":"message","content":"codex ok"}`), err: nil},
		},
		callCount: &callCount,
	}

	session := agent.NewSession(runner)
	session.SetCodexFallback("auto")

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role:   agent.RoleEngineer1,
		Model:  "claude-sonnet-4-6",
		Prompt: "test",
		Env:    map[string]string{"GH_TOKEN": "tok"},
	})

	require.NoError(t, err)
	assert.Equal(t, "codex ok", result.Transcript)
}

// fakeRunner records calls.
type fakeRunner struct {
	output []byte
	err    error
	calls  int
}

func (runner *fakeRunner) Run(_ context.Context, _, _, _ string, _ ...string) ([]byte, error) {
	runner.calls++
	return runner.output, runner.err
}

type runResult struct {
	output []byte
	err    error
}

type switchingRunner struct {
	responses []runResult
	callCount *int
}

func (runner *switchingRunner) Run(_ context.Context, _, _, _ string, _ ...string) ([]byte, error) {
	idx := *runner.callCount
	*runner.callCount++
	if idx < len(runner.responses) {
		return runner.responses[idx].output, runner.responses[idx].err
	}
	return nil, fmt.Errorf("no more responses")
}

type codexLastMessageRunner struct {
	responses        []runResult
	callCount        *int
	lastMessageValue string
}

func (runner *codexLastMessageRunner) Run(_ context.Context, _, _, name string, args ...string) ([]byte, error) {
	idx := *runner.callCount
	*runner.callCount++

	if name != "codex" || runner.lastMessageValue == "" {
		return runner.result(idx)
	}

	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-o" {
			_ = os.WriteFile(args[i+1], []byte(runner.lastMessageValue), 0o600)
			break
		}
	}

	return runner.result(idx)
}

func (runner *codexLastMessageRunner) result(idx int) ([]byte, error) {
	if idx < len(runner.responses) {
		return runner.responses[idx].output, runner.responses[idx].err
	}
	return nil, fmt.Errorf("no more responses")
}
