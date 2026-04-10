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

func TestBridge_Chat_DoubleSwap_StaysOnFallback(t *testing.T) {
	t.Parallel()
	active := &fakeRuntime{name: "claude", sendErr: fmt.Errorf("rate limit 429")}
	fallback := &fakeRuntime{name: "codex", sendResponse: "ok"}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, fallback)
	_, err := bridge.Chat(context.Background(), "test1", "")
	require.NoError(t, err)
	assert.True(t, bridge.IsSwapped())
	_, err2 := bridge.Chat(context.Background(), "test2", "")
	require.NoError(t, err2)
	assert.Equal(t, "codex", bridge.Active().Name())
}

func TestBridge_ResetSwap_ThenChat(t *testing.T) {
	t.Parallel()
	active := &fakeRuntime{name: "claude", sendErr: fmt.Errorf("rate limit 429")}
	fallback := &fakeRuntime{name: "codex", sendResponse: "ok"}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, fallback)
	_, _ = bridge.Chat(context.Background(), "test", "")
	bridge.ResetSwap()
	assert.False(t, bridge.IsSwapped())
	assert.Equal(t, "claude", bridge.Active().Name())
}

func TestCodexRuntime_Send_Error_ReturnsPartialTranscript(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{
		output: []byte(`{"type":"message","content":"partial"}`),
		err:    fmt.Errorf("exit status 1"),
	}
	rt := runtime.NewCodexRuntime(runner, "gpt-5-codex", "/tmp")
	response, err := rt.Send(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, response, "partial")
}

func TestBridge_Chat_NilFallback_RateLimit_ReturnsError(t *testing.T) {
	t.Parallel()
	active := &fakeRuntime{name: "claude", sendErr: fmt.Errorf("rate limit 429")}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer2, active, nil)
	_, err := bridge.Chat(context.Background(), "test", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no fallback")
}
