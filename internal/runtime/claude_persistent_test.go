package runtime_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTmux records tmux operations without executing them.
type fakeTmux struct {
	mu       sync.Mutex
	sessions map[string]bool
	killed   []string
	created  []string
	sentKeys []string
}

func newFakeTmux() *fakeTmux {
	return &fakeTmux{sessions: make(map[string]bool)}
}

func (f *fakeTmux) NewSession(name, _, _ string, _ ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[name] = true
	f.created = append(f.created, name)
	return nil
}

func (f *fakeTmux) HasSession(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[name]
}

func (f *fakeTmux) KillSession(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, name)
	f.killed = append(f.killed, name)
	return nil
}

func (f *fakeTmux) SendKeys(_, keys string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentKeys = append(f.sentKeys, keys)
	return nil
}

func TestClaudePersistent_Name(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "engineer-1", "claude-sonnet-4-6", dir)

	assert.Equal(t, "claude-persistent", rt.Name())
}

func TestClaudePersistent_SupportsHooks(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "pm", "claude-haiku-4-5-20251001", dir)

	assert.True(t, rt.SupportsHooks())
}

func TestClaudePersistent_Start_CreatesTmuxSession(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "engineer-1", "claude-sonnet-4-6", dir)

	err := rt.Start(context.Background(), runtime.StartConfig{})
	require.NoError(t, err)

	assert.True(t, rt.IsAlive())
	assert.Contains(t, tmux.created, "squad0-engineer-1")
}

func TestClaudePersistent_Start_KillsStaleSession(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	tmux.sessions["squad0-engineer-2"] = true // stale

	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "engineer-2", "claude-sonnet-4-6", dir)

	err := rt.Start(context.Background(), runtime.StartConfig{})
	require.NoError(t, err)

	// Old session should have been killed, new one created.
	assert.Contains(t, tmux.killed, "squad0-engineer-2")
	assert.True(t, rt.IsAlive())
}

func TestClaudePersistent_Stop_KillsSession(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "reviewer", "claude-opus-4-6", dir)

	_ = rt.Start(context.Background(), runtime.StartConfig{})
	assert.True(t, rt.IsAlive())

	err := rt.Stop()
	require.NoError(t, err)
	assert.False(t, rt.IsAlive())
}

func TestClaudePersistent_Stop_NoSession_NoError(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "designer", "claude-sonnet-4-6", dir)

	assert.NoError(t, rt.Stop())
}

func TestClaudePersistent_Send_CallsSendKeys(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "engineer-1", "claude-sonnet-4-6", dir)
	rt.SetTimeout(500 * time.Millisecond) // short timeout — no real Claude to respond

	_ = rt.Start(context.Background(), runtime.StartConfig{})

	// Send will call SendKeys then timeout waiting for a response
	// (no real Claude process). The important thing: SendKeys was called.
	_, _ = rt.Send(context.Background(), "what do you think?")

	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	assert.NotEmpty(t, tmux.sentKeys, "Send should call SendKeys to nudge the session")
	assert.Contains(t, tmux.sentKeys[0], "what do you think?")
}

func TestClaudePersistent_Send_DeadSession_SelfHeals(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "engineer-3", "claude-sonnet-4-6", dir)
	rt.SetTimeout(500 * time.Millisecond)

	// Don't call Start — session is dead. Send should self-heal.
	_, _ = rt.Send(context.Background(), "are you there?")

	// Session should have been started via self-heal.
	assert.True(t, rt.IsAlive())
}

func TestClaudePersistent_Send_Timeout_ReturnsError(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "tech-lead", "claude-opus-4-6", dir)
	rt.SetTimeout(500 * time.Millisecond)

	_ = rt.Start(context.Background(), runtime.StartConfig{})

	// No response written — should timeout.
	_, err := rt.Send(context.Background(), "no response expected")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "waiting for response")
}

func TestClaudePersistent_Start_WithConfigOverrides(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "engineer-1", "claude-sonnet-4-6", dir)

	err := rt.Start(context.Background(), runtime.StartConfig{
		Model:   "claude-opus-4-6",
		WorkDir: "/custom/dir",
	})
	require.NoError(t, err)
	assert.True(t, rt.IsAlive())
}

func TestClaudePersistent_IsAlive_NoSession_ReturnsFalse(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "pm", "haiku", dir)

	assert.False(t, rt.IsAlive())
}

func TestClaudePersistent_SessionName(t *testing.T) {
	t.Parallel()

	tmux := newFakeTmux()
	dir := t.TempDir()
	inbox, _ := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	rt := runtime.NewClaudePersistentRuntime(tmux, inbox, "pm", "claude-haiku-4-5-20251001", dir)

	assert.Equal(t, "squad0-pm", rt.SessionNameForTest())
}
