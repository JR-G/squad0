package runtime_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInbox_Enqueue_CreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inbox, err := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	require.NoError(t, err)

	id, enqErr := inbox.Enqueue("hello agent")
	require.NoError(t, enqErr)
	assert.NotEmpty(t, id)
}

func TestInbox_Drain_ReturnsMessages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inbox, err := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	require.NoError(t, err)

	_, _ = inbox.Enqueue("first")
	_, _ = inbox.Enqueue("second")

	messages, drainErr := inbox.Drain()
	require.NoError(t, drainErr)
	assert.Len(t, messages, 2)
	assert.Equal(t, "first", messages[0].Prompt)
	assert.Equal(t, "second", messages[1].Prompt)
}

func TestInbox_Drain_ClearsQueue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inbox, err := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	require.NoError(t, err)

	_, _ = inbox.Enqueue("message")
	_, drainErr := inbox.Drain()
	require.NoError(t, drainErr)

	// Second drain should be empty.
	messages, secondErr := inbox.Drain()
	require.NoError(t, secondErr)
	assert.Empty(t, messages)
}

func TestInbox_Drain_Empty_ReturnsNil(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inbox, err := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	require.NoError(t, err)

	messages, drainErr := inbox.Drain()
	require.NoError(t, drainErr)
	assert.Empty(t, messages)
}

func TestInbox_WriteAndWaitResponse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inbox, err := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	require.NoError(t, err)

	id, _ := inbox.Enqueue("prompt")

	// Write response in background.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = inbox.WriteResponse(id, "the answer is 42")
	}()

	response, waitErr := inbox.WaitForResponse(id, 2*time.Second)
	require.NoError(t, waitErr)
	assert.Equal(t, "the answer is 42", response)
}

func TestInbox_WaitResponse_Timeout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inbox, err := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	require.NoError(t, err)

	// No response written — should timeout.
	_, waitErr := inbox.WaitForResponse("nonexistent", 500*time.Millisecond)
	assert.Error(t, waitErr)
	assert.Contains(t, waitErr.Error(), "timeout")
}

func TestInbox_InboxDir_ReturnsPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inDir := filepath.Join(dir, "in")
	inbox, err := runtime.NewInbox(inDir, filepath.Join(dir, "out"))
	require.NoError(t, err)
	assert.Equal(t, inDir, inbox.InboxDir())
}

func TestInbox_OutboxDir_ReturnsPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")
	inbox, err := runtime.NewInbox(filepath.Join(dir, "in"), outDir)
	require.NoError(t, err)
	assert.Equal(t, outDir, inbox.OutboxDir())
}

func TestInbox_Enqueue_WritesValidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inbox, err := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	require.NoError(t, err)

	_, enqErr := inbox.Enqueue("test prompt")
	require.NoError(t, enqErr)

	messages, drainErr := inbox.Drain()
	require.NoError(t, drainErr)
	require.Len(t, messages, 1)
	assert.Equal(t, "test prompt", messages[0].Prompt)
	assert.NotEmpty(t, messages[0].ID)
	assert.False(t, messages[0].Timestamp.IsZero())
}

func TestInbox_WriteResponse_CreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inbox, err := runtime.NewInbox(filepath.Join(dir, "in"), filepath.Join(dir, "out"))
	require.NoError(t, err)

	writeErr := inbox.WriteResponse("test-id", "response text")
	require.NoError(t, writeErr)

	// Should be immediately readable.
	response, waitErr := inbox.WaitForResponse("test-id", time.Second)
	require.NoError(t, waitErr)
	assert.Equal(t, "response text", response)
}

func TestFormatDrained_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", runtime.FormatDrained(nil))
}

func TestFormatDrained_WrapsInSystemReminder(t *testing.T) {
	t.Parallel()

	messages := []runtime.InboxMessage{
		{ID: "1", Prompt: "hello agent"},
		{ID: "2", Prompt: "another message"},
	}

	result := runtime.FormatDrained(messages)
	assert.Contains(t, result, "<system-reminder>")
	assert.Contains(t, result, "hello agent")
	assert.Contains(t, result, "another message")
	assert.Contains(t, result, "</system-reminder>")
}
