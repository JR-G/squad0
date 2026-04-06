package runtime_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/JR-G/squad0/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	mu      sync.Mutex
	output  []byte
	err     error
	calls   []string
	runHook func(name string, args []string)
}

func (r *fakeRunner) Run(_ context.Context, stdin, _, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runHook != nil {
		r.runHook(name, args)
	}
	r.calls = append(r.calls, name+" "+fmt.Sprintf("%v", args))
	return r.output, r.err
}

func TestCodexRuntime_Name(t *testing.T) {
	t.Parallel()

	rt := runtime.NewCodexRuntime(&fakeRunner{}, "gpt-5-codex", "/tmp")
	assert.Equal(t, "codex", rt.Name())
}

func TestCodexRuntime_SupportsHooks_ReturnsFalse(t *testing.T) {
	t.Parallel()

	rt := runtime.NewCodexRuntime(&fakeRunner{}, "gpt-5-codex", "/tmp")
	assert.False(t, rt.SupportsHooks())
}

func TestCodexRuntime_IsAlive_ReturnsFalse(t *testing.T) {
	t.Parallel()

	rt := runtime.NewCodexRuntime(&fakeRunner{}, "gpt-5-codex", "/tmp")
	assert.False(t, rt.IsAlive())
}

func TestCodexRuntime_Start_IsNoOp(t *testing.T) {
	t.Parallel()

	rt := runtime.NewCodexRuntime(&fakeRunner{}, "gpt-5-codex", "/tmp")
	err := rt.Start(context.Background(), runtime.StartConfig{})
	assert.NoError(t, err)
}

func TestCodexRuntime_Stop_IsNoOp(t *testing.T) {
	t.Parallel()

	rt := runtime.NewCodexRuntime(&fakeRunner{}, "gpt-5-codex", "/tmp")
	assert.NoError(t, rt.Stop())
}

func TestCodexRuntime_Send_ReturnsTranscript(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		output: []byte(`{"type":"message","content":"hello from codex"}` + "\n"),
	}
	rt := runtime.NewCodexRuntime(runner, "gpt-5-codex", "/tmp")

	result, err := rt.Send(context.Background(), "say hello")
	require.NoError(t, err)
	assert.Equal(t, "hello from codex", result)
}

func TestCodexRuntime_Send_ProcessError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		output: []byte(`{"type":"error","content":"fail"}`),
		err:    fmt.Errorf("exit status 1"),
	}
	rt := runtime.NewCodexRuntime(runner, "gpt-5-codex", "/tmp")

	_, err := rt.Send(context.Background(), "fail")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "codex send")
}

func TestCodexRuntime_Send_EmptyOutput_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{output: []byte("")}
	rt := runtime.NewCodexRuntime(runner, "gpt-5-codex", "/tmp")

	_, err := rt.Send(context.Background(), "empty")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func TestCodexRuntime_Send_UsesLastMessageFileWhenStdoutEmpty(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		output: []byte(`{"type":"thread.started"}` + "\n"),
		runHook: func(_ string, args []string) {
			for i := 0; i < len(args)-1; i++ {
				if args[i] == "-o" {
					_ = os.WriteFile(args[i+1], []byte("hello from last-message file"), 0o600)
					return
				}
			}
		},
	}
	rt := runtime.NewCodexRuntime(runner, "gpt-5-codex", "/tmp")

	result, err := rt.Send(context.Background(), "say hello")
	require.NoError(t, err)
	assert.Equal(t, "hello from last-message file", result)
}
