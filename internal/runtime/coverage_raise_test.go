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

func TestBridge_Chat_NilError_NotTimeout(t *testing.T) {
	t.Parallel()
	active := &fakeRuntime{name: "claude", sendResponse: "ok"}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, nil)
	result, err := bridge.Chat(context.Background(), "hi", "", "")
	require.NoError(t, err)
	assert.Equal(t, "ok", result)
}

func TestBridge_ResetSwap_WhenNotSwapped_Noop(t *testing.T) {
	t.Parallel()
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, &fakeRuntime{name: "claude"}, &fakeRuntime{name: "codex"})
	bridge.ResetSwap()
	assert.False(t, bridge.IsSwapped())
	assert.Equal(t, "claude", bridge.Active().Name())
}

func TestCodex_Send_EmptyResponse_ReturnsError(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{output: []byte("")}
	rt := runtime.NewCodexRuntime(runner, "gpt-5", t.TempDir())
	_, err := rt.Send(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func TestCodex_Send_RunnerError_ReturnsTranscript(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{output: []byte("partial output"), err: fmt.Errorf("process exited")}
	rt := runtime.NewCodexRuntime(runner, "gpt-5", t.TempDir())
	result, err := rt.Send(context.Background(), "test")
	assert.Error(t, err)
	assert.NotEmpty(t, result)
}
