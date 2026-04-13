package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestInbox_Drain_SkipsCorruptJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	_, _ = inbox.Enqueue("valid prompt")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "in", "corrupt-123.json"), []byte(`not json{{{`), 0o644))
	messages, _ := inbox.Drain()
	assert.Len(t, messages, 1)
	assert.Equal(t, "valid prompt", messages[0].Prompt)
}

func TestInbox_Drain_SkipsSubdirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	_, _ = inbox.Enqueue("real message")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "in", "subdir"), 0o755))
	messages, _ := inbox.Drain()
	assert.Len(t, messages, 1)
}

func TestInbox_Drain_SkipsNonJSONFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	_, _ = inbox.Enqueue("real message")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "in", "notes.txt"), []byte("hello"), 0o644))
	messages, _ := inbox.Drain()
	assert.Len(t, messages, 1)
}

func TestInbox_WaitForResponse_CorruptJSON_ReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), outDir)
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "bad-id-response.json"), []byte(`not json`), 0o644))
	_, err := inbox.WaitForResponse("bad-id", time.Second)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing response")
}

func TestInbox_WaitForSignal_CorruptJSON_ReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), outDir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(outDir, "latest-response.json"), []byte(`{corrupt`), 0o644)
	}()
	_, err := inbox.WaitForSignal(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing signal")
}

func TestWriteHookSettings_CreatesValidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, runtime.WriteHookSettings(dir, "engineer-1"))
	data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	var parsed map[string]interface{}
	assert.NoError(t, json.Unmarshal(data, &parsed))
	assert.Contains(t, parsed, "hooks")
}

func TestFormatDrained_SingleMessage(t *testing.T) {
	t.Parallel()
	messages := []runtime.InboxMessage{{ID: "1", Prompt: "do the thing"}}
	assert.Contains(t, runtime.FormatDrained(messages), "do the thing")
}

func TestNewInbox_OutboxDirInvalid_ReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := runtime.NewInbox(filepath.Join(dir, "in"), "/dev/null/outbox")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creating outbox dir")
}

func TestClaudePersistent_Send_HappyPath_ReturnsResponse(t *testing.T) {
	t.Parallel()
	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "engineer-1", "sonnet", dir)
	rt.SetTimeout(2 * time.Second)
	require.NoError(t, rt.Start(context.Background(), runtime.StartConfig{}))
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "out", "latest-response.json"), []byte(`{"response":"hello from Claude"}`), 0o644)
	}()
	response, err := rt.Send(context.Background(), "test prompt")
	require.NoError(t, err)
	assert.Equal(t, "hello from Claude", response)
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
