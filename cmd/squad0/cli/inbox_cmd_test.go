package cli

import (
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunInboxDrain_EmptyInbox_NoError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := runInboxDrain("engineer-1", dir)
	assert.NoError(t, err)
}

func TestRunInboxDrain_InvalidPath_ReturnsError(t *testing.T) {
	t.Parallel()

	// /dev/null is a file, not a directory — should fail.
	err := runInboxDrain("test", "/dev/null")
	assert.Error(t, err)
}

func TestRunInboxDrain_WithMessages_NoError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "inbox", "engineer-1")
	outboxDir := filepath.Join(dir, "outbox", "engineer-1")

	inbox, err := runtime.NewInbox(inboxDir, outboxDir)
	require.NoError(t, err)

	_, _ = inbox.Enqueue("test message")

	drainErr := runInboxDrain("engineer-1", dir)
	assert.NoError(t, drainErr)
}
