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

// fakeRuntime implements runtime.Runtime for testing.
type fakeRuntime struct {
	name         string
	hooks        bool
	alive        bool
	sendResponse string
	sendErr      error
	sendCalls    int
	stopErr      error
}

func (f *fakeRuntime) Start(_ context.Context, _ runtime.StartConfig) error { return nil }

func (f *fakeRuntime) Send(_ context.Context, _ string) (string, error) {
	f.sendCalls++
	return f.sendResponse, f.sendErr
}

func (f *fakeRuntime) IsAlive() bool       { return f.alive }
func (f *fakeRuntime) Stop() error         { return f.stopErr }
func (f *fakeRuntime) Name() string        { return f.name }
func (f *fakeRuntime) SupportsHooks() bool { return f.hooks }

func TestBridge_Chat_HappyPath(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{name: "claude", sendResponse: "hello"}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, nil)

	response, err := bridge.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "hello", response)
	assert.Equal(t, 1, active.sendCalls)
}

func TestBridge_Chat_RateLimit_SwapsToFallback(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{
		name:    "claude",
		sendErr: fmt.Errorf("rate limit exceeded (429)"),
	}
	fallback := &fakeRuntime{
		name:         "codex",
		sendResponse: "fallback response",
	}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer2, active, fallback)

	response, err := bridge.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "fallback response", response)
	assert.True(t, bridge.IsSwapped())
}

func TestBridge_Chat_RateLimit_NoFallback_ReturnsError(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{
		name:    "claude",
		sendErr: fmt.Errorf("rate limit exceeded (429)"),
	}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, nil)

	_, err := bridge.Chat(context.Background(), "hi")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no fallback")
}

func TestBridge_Chat_NonRateError_DoesNotSwap(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{
		name:    "claude",
		sendErr: fmt.Errorf("session crashed"),
	}
	fallback := &fakeRuntime{
		name:         "codex",
		sendResponse: "should not reach",
	}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, fallback)

	_, err := bridge.Chat(context.Background(), "hi")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chat via claude")
	assert.False(t, bridge.IsSwapped())
	assert.Equal(t, 0, fallback.sendCalls)
}

func TestBridge_Chat_FallbackAlsoFails_ReturnsError(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{
		name:    "claude",
		sendErr: fmt.Errorf("rate limit (429)"),
	}
	fallback := &fakeRuntime{
		name:    "codex",
		sendErr: fmt.Errorf("codex also failed"),
	}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer3, active, fallback)

	_, err := bridge.Chat(context.Background(), "hi")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fallback codex also failed")
}

func TestBridge_ResetSwap(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{
		name:    "claude",
		sendErr: fmt.Errorf("rate limit 429"),
	}
	fallback := &fakeRuntime{name: "codex", sendResponse: "ok"}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, fallback)

	_, _ = bridge.Chat(context.Background(), "hi")
	assert.True(t, bridge.IsSwapped())

	bridge.ResetSwap()
	assert.False(t, bridge.IsSwapped())
}

func TestBridge_Active_ReturnsActiveRuntime(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{name: "claude"}
	bridge := runtime.NewSessionBridge(agent.RolePM, active, nil)

	assert.Equal(t, "claude", bridge.Active().Name())
}

func TestBridge_Role_ReturnsRole(t *testing.T) {
	t.Parallel()

	bridge := runtime.NewSessionBridge(agent.RoleDesigner, &fakeRuntime{}, nil)
	assert.Equal(t, agent.RoleDesigner, bridge.Role())
}

func TestBridge_Stop_StopsBothRuntimes(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{name: "claude"}
	fallback := &fakeRuntime{name: "codex"}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, fallback)

	assert.NoError(t, bridge.Stop())
}

func TestBridge_Stop_ActiveError_ReturnsError(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{name: "claude", stopErr: fmt.Errorf("stop failed")}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, nil)

	assert.Error(t, bridge.Stop())
}

func TestBridge_Stop_NilFallback_NoError(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{name: "claude"}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, nil)

	assert.NoError(t, bridge.Stop())
}

func TestBridge_Stop_FallbackError_ReturnsError(t *testing.T) {
	t.Parallel()

	active := &fakeRuntime{name: "claude"}
	fallback := &fakeRuntime{name: "codex", stopErr: fmt.Errorf("codex stop failed")}
	bridge := runtime.NewSessionBridge(agent.RoleEngineer1, active, fallback)

	assert.Error(t, bridge.Stop())
}

func TestBridge_IsSwapped_InitiallyFalse(t *testing.T) {
	t.Parallel()

	bridge := runtime.NewSessionBridge(agent.RolePM, &fakeRuntime{}, nil)
	assert.False(t, bridge.IsSwapped())
}
