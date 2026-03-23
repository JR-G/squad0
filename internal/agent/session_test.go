package agent_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProcessRunner struct {
	output []byte
	err    error
	calls  []fakeProcessCall
}

type fakeProcessCall struct {
	stdin string
	name  string
	args  []string
}

func (runner *fakeProcessRunner) Run(_ context.Context, stdin, _ /* workingDir */, name string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, fakeProcessCall{stdin: stdin, name: name, args: args})
	return runner.output, runner.err
}

func TestSession_Run_CapturesOutput(t *testing.T) {
	t.Parallel()

	output := `{"type":"assistant","content":"hello"}` + "\n" +
		`{"type":"result","result":"done"}` + "\n"
	runner := &fakeProcessRunner{output: []byte(output)}
	session := agent.NewSession(runner)

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role:   agent.RoleEngineer1,
		Model:  "claude-sonnet-4-6",
		Prompt: "do something",
	})

	require.NoError(t, err)
	assert.Equal(t, output, result.RawOutput)
	assert.Len(t, result.Messages, 2)
}

func TestSession_Run_PassesCorrectArgs(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{output: []byte("{}\n")}
	session := agent.NewSession(runner)

	_, _ = session.Run(context.Background(), agent.SessionConfig{
		Role:   agent.RolePM,
		Model:  "claude-haiku-4-5-20251001",
		Prompt: "run standup",
	})

	require.Len(t, runner.calls, 1)
	call := runner.calls[0]
	assert.Equal(t, "claude", call.name)
	assert.Contains(t, call.args, "-p")
	assert.Contains(t, call.args, "--model")
	assert.Contains(t, call.args, "claude-haiku-4-5-20251001")
	assert.Contains(t, call.args, "--output-format")
	assert.Contains(t, call.args, "stream-json")
	assert.Contains(t, call.args, "--dangerously-skip-permissions")
	assert.Equal(t, "run standup", call.stdin)
}

func TestSession_Run_ProcessError_ReturnsErrorWithResult(t *testing.T) {
	t.Parallel()

	output := `{"type":"error","content":"context exhausted"}` + "\n"
	runner := &fakeProcessRunner{
		output: []byte(output),
		err:    fmt.Errorf("exit status 1"),
	}
	session := agent.NewSession(runner)

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "task",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude session failed")
	assert.NotEmpty(t, result.RawOutput)
	assert.Len(t, result.Messages, 1)
}

func TestSession_Run_ExtractsTranscript_FromResult(t *testing.T) {
	t.Parallel()

	output := `{"type":"result","result":"Bug fixed."}` + "\n"
	runner := &fakeProcessRunner{output: []byte(output)}
	session := agent.NewSession(runner)

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "fix bug",
	})

	require.NoError(t, err)
	assert.Equal(t, "Bug fixed.", result.Transcript)
}

func TestSession_Run_ExtractsTranscript_FromAssistant(t *testing.T) {
	t.Parallel()

	output := `{"type":"assistant","message":{"content":[{"type":"text","text":"I fixed it."}]}}` + "\n"
	runner := &fakeProcessRunner{output: []byte(output)}
	session := agent.NewSession(runner)

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "fix bug",
	})

	require.NoError(t, err)
	assert.Equal(t, "I fixed it.", result.Transcript)
}

func TestSession_Run_ExtractsTranscript_PrefersResult(t *testing.T) {
	t.Parallel()

	output := `{"type":"assistant","message":{"content":[{"type":"text","text":"Working on it."}]}}` + "\n" +
		`{"type":"result","result":"Done."}` + "\n"
	runner := &fakeProcessRunner{output: []byte(output)}
	session := agent.NewSession(runner)

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "fix bug",
	})

	require.NoError(t, err)
	assert.Equal(t, "Done.", result.Transcript)
}

func TestSession_Run_EmptyOutput_ReturnsEmptyResult(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{output: []byte("")}
	session := agent.NewSession(runner)

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RolePM, Model: "claude-haiku-4-5-20251001", Prompt: "noop",
	})

	require.NoError(t, err)
	assert.Empty(t, result.Messages)
	assert.Empty(t, result.Transcript)
}

func TestSession_Run_InvalidJSON_SkipsLines(t *testing.T) {
	t.Parallel()

	output := "not json\n" +
		`{"type":"result","result":"ok"}` + "\n" +
		"also not json\n"
	runner := &fakeProcessRunner{output: []byte(output)}
	session := agent.NewSession(runner)

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "task",
	})

	require.NoError(t, err)
	assert.Len(t, result.Messages, 1)
}

func TestSession_Run_MaxTurns_IncludedInArgs(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{output: []byte("{}\n")}
	session := agent.NewSession(runner)

	_, _ = session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6",
		Prompt: "task", MaxTurnDuration: 50,
	})

	require.Len(t, runner.calls, 1)
	assert.Contains(t, runner.calls[0].args, "--max-turns")
	assert.Contains(t, runner.calls[0].args, "50")
}
