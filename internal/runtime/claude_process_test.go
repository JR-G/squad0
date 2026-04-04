package runtime_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeProcessRuntime_Name(t *testing.T) {
	t.Parallel()

	session := agent.NewSession(&fakeRunner{})
	rt := runtime.NewClaudeProcessRuntime(session, "claude-sonnet-4-6", "/tmp")
	assert.Equal(t, "claude", rt.Name())
}

func TestClaudeProcessRuntime_SupportsHooks_ReturnsFalse(t *testing.T) {
	t.Parallel()

	session := agent.NewSession(&fakeRunner{})
	rt := runtime.NewClaudeProcessRuntime(session, "claude-sonnet-4-6", "/tmp")
	assert.False(t, rt.SupportsHooks())
}

func TestClaudeProcessRuntime_IsAlive_ReturnsFalse(t *testing.T) {
	t.Parallel()

	session := agent.NewSession(&fakeRunner{})
	rt := runtime.NewClaudeProcessRuntime(session, "claude-sonnet-4-6", "/tmp")
	assert.False(t, rt.IsAlive())
}

func TestClaudeProcessRuntime_Start_IsNoOp(t *testing.T) {
	t.Parallel()

	session := agent.NewSession(&fakeRunner{})
	rt := runtime.NewClaudeProcessRuntime(session, "claude-sonnet-4-6", "/tmp")
	assert.NoError(t, rt.Start(context.Background(), runtime.StartConfig{}))
}

func TestClaudeProcessRuntime_Stop_IsNoOp(t *testing.T) {
	t.Parallel()

	session := agent.NewSession(&fakeRunner{})
	rt := runtime.NewClaudeProcessRuntime(session, "claude-sonnet-4-6", "/tmp")
	assert.NoError(t, rt.Stop())
}

func TestClaudeProcessRuntime_Send_ReturnsTranscript(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		output: []byte(`{"type":"result","result":"implementation complete"}` + "\n"),
	}
	session := agent.NewSession(runner)
	rt := runtime.NewClaudeProcessRuntime(session, "claude-sonnet-4-6", "/tmp")

	result, err := rt.Send(context.Background(), "implement the feature")
	require.NoError(t, err)
	assert.Equal(t, "implementation complete", result)
}

func TestClaudeProcessRuntime_Send_Error_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		output: []byte(`{"type":"error","content":"fail"}`),
		err:    fmt.Errorf("exit status 1"),
	}
	session := agent.NewSession(runner)
	rt := runtime.NewClaudeProcessRuntime(session, "claude-sonnet-4-6", "/tmp")

	_, err := rt.Send(context.Background(), "fail")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claude process send")
}
