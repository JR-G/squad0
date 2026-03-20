package logging_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionWriter_WriteSession_CreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writer := logging.NewSessionWriter(dir)

	path, err := writer.WriteSession("engineer-1", "session output here")

	require.NoError(t, err)
	assert.FileExists(t, path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "session output here", string(content))
}

func TestSessionWriter_WriteSession_CreatesAgentDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writer := logging.NewSessionWriter(dir)

	_, err := writer.WriteSession("pm", "output")

	require.NoError(t, err)

	agentDir := filepath.Join(dir, "pm")
	info, err := os.Stat(agentDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestSessionWriter_WriteSession_MultipleSessions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writer := logging.NewSessionWriter(dir)

	path1, err := writer.WriteSession("engineer-1", "first session")
	require.NoError(t, err)

	path2, err := writer.WriteSession("engineer-1", "second session")
	require.NoError(t, err)

	assert.NotEqual(t, path1, path2)
	assert.FileExists(t, path1)
	assert.FileExists(t, path2)
}

func TestSessionWriter_WriteSession_DifferentAgents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writer := logging.NewSessionWriter(dir)

	path1, err := writer.WriteSession("engineer-1", "output 1")
	require.NoError(t, err)

	path2, err := writer.WriteSession("engineer-2", "output 2")
	require.NoError(t, err)

	assert.Contains(t, path1, "engineer-1")
	assert.Contains(t, path2, "engineer-2")
}

func TestSessionWriter_WriteSession_InvalidDir_ReturnsError(t *testing.T) {
	t.Parallel()

	writer := logging.NewSessionWriter("/dev/null/impossible")

	_, err := writer.WriteSession("agent", "output")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating session directory")
}

func TestSessionWriter_SessionDir_ReturnsCorrectPath(t *testing.T) {
	t.Parallel()

	writer := logging.NewSessionWriter("/data/sessions")

	assert.Equal(t, "/data/sessions/engineer-1", writer.SessionDir("engineer-1"))
	assert.Equal(t, "/data/sessions/pm", writer.SessionDir("pm"))
}

func TestSessionWriter_WriteSession_LargeOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writer := logging.NewSessionWriter(dir)

	largeOutput := make([]byte, 1024*1024)
	for i := range largeOutput {
		largeOutput[i] = 'x'
	}

	path, err := writer.WriteSession("engineer-1", string(largeOutput))

	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(1024*1024), info.Size())
}
