package logging_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger_RotationFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	// Create a directory, then make a file where the log file would go
	// so that the initial rotateIfNeeded fails when it tries to open
	// the file as a directory-based path.
	dir := t.TempDir()

	// Create a read-only directory so file creation fails.
	readOnlyDir := filepath.Join(dir, "readonly")
	require.NoError(t, os.MkdirAll(readOnlyDir, 0o755))
	require.NoError(t, os.Chmod(readOnlyDir, 0o444))

	t.Cleanup(func() {
		// Restore permissions so TempDir cleanup works.
		_ = os.Chmod(readOnlyDir, 0o755)
	})

	_, err := logging.NewLogger(readOnlyDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening initial log file")
}

func TestLogger_Info_AfterClose_StillWorks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)

	// Close the logger to set current to nil.
	require.NoError(t, logger.Close())

	// Info should trigger rotateIfNeeded which re-opens the file.
	logger.Info("engineer-1", "session_start", "post-close logging")
	require.NoError(t, logger.Close())

	content := readLogFile(t, dir)
	assert.Contains(t, content, "post-close logging")
}

func TestLogger_LogAfterRotationFailure_SilentlyDrops(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)

	// Close the underlying file, then make the directory read-only
	// so rotateIfNeeded fails. The log method should not panic.
	require.NoError(t, logger.Close())

	require.NoError(t, os.Chmod(dir, 0o444))
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	// This should not panic even though rotation fails.
	assert.NotPanics(t, func() {
		logger.Info("pm", "tick", "this will be dropped")
	})
}

func TestLogger_WarnAndError_WriteToSameFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	logger.Info("engineer-1", "start", "info entry")
	logger.Warn("pm", "rate_limit", "warn entry")
	logger.Error("reviewer", "crash", "error entry")
	_ = logger.Close()

	content := readLogFile(t, dir)
	assert.Contains(t, content, "info entry")
	assert.Contains(t, content, "warn entry")
	assert.Contains(t, content, "error entry")
}

func TestSessionWriter_WriteSession_ReadOnlyDir_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create the agent directory but make it read-only so WriteFile fails.
	agentDir := filepath.Join(dir, "engineer-1")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.Chmod(agentDir, 0o444))

	t.Cleanup(func() {
		_ = os.Chmod(agentDir, 0o755)
	})

	writer := logging.NewSessionWriter(dir)

	_, err := writer.WriteSession("engineer-1", "should fail")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing session file")
}
