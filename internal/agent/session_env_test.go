package agent_test

import (
	"context"
	"os"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectSession_WithGHToken_SetsEnv(t *testing.T) {
	// Not parallel — modifies env.
	original := os.Getenv("GH_TOKEN")
	defer restoreGHToken(original)

	var capturedEnv string
	runner := &fakeAgentRunner{
		runFn: func(_ context.Context, _, _, _ string, _ ...string) ([]byte, error) {
			capturedEnv = os.Getenv("GH_TOKEN")
			return []byte(`{"type":"result","result":"done"}` + "\n"), nil
		},
	}

	session := agent.NewSession(runner)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/pm.md", []byte("test"), 0o644))

	a := agent.NewAgent(agent.RolePM, "test", session, agent.NewPersonalityLoader(dir), nil, nil, nil, nil)
	a.SetGHToken("ghs_app_token_123")

	_, err := a.DirectSession(context.Background(), "test prompt")
	require.NoError(t, err)

	assert.Equal(t, "ghs_app_token_123", capturedEnv)

	// After the session, the env should be restored.
	assert.Equal(t, original, os.Getenv("GH_TOKEN"))
}

func TestDirectSession_WithoutGHToken_NoEnvChange(t *testing.T) {
	// Not parallel — reads env.
	original := os.Getenv("GH_TOKEN")

	runner := &fakeAgentRunner{
		runFn: func(_ context.Context, _, _, _ string, _ ...string) ([]byte, error) {
			return []byte(`{"type":"result","result":"done"}` + "\n"), nil
		},
	}

	session := agent.NewSession(runner)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/pm.md", []byte("test"), 0o644))

	a := agent.NewAgent(agent.RolePM, "test", session, agent.NewPersonalityLoader(dir), nil, nil, nil, nil)

	_, err := a.DirectSession(context.Background(), "test prompt")
	require.NoError(t, err)

	assert.Equal(t, original, os.Getenv("GH_TOKEN"))
}

type fakeAgentRunner struct {
	runFn func(ctx context.Context, stdin, workingDir, name string, args ...string) ([]byte, error)
}

func (r *fakeAgentRunner) Run(ctx context.Context, stdin, workingDir, name string, args ...string) ([]byte, error) {
	return r.runFn(ctx, stdin, workingDir, name, args...)
}

func restoreGHToken(original string) {
	if original == "" {
		_ = os.Unsetenv("GH_TOKEN")
		return
	}
	_ = os.Setenv("GH_TOKEN", original)
}
