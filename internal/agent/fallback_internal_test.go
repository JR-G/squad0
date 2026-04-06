package agent

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCodexTranscript_PrefersStdoutOverFile(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp("", "squad0-codex-last-message-*.txt")
	require.NoError(t, err)
	path := file.Name()
	_, err = file.WriteString("from file")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	t.Cleanup(func() { _ = os.Remove(path) })

	result := ResolveCodexTranscript(`{"type":"message","content":"from stdout"}`+"\n", path)
	assert.Equal(t, "from stdout", result)
}

func TestResolveCodexTranscript_MissingFile_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := ResolveCodexTranscript(`{"type":"thread.started"}`+"\n", "/tmp/does-not-exist-squad0")
	assert.Empty(t, result)
}

func TestExtractCodexText_ArrayVariants(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`[{"content":"alpha"},{"message":"beta"},{"result":"gamma"}]`)
	result, ok := extractCodexText(raw)
	require.True(t, ok)
	assert.Equal(t, "alpha\nbeta\ngamma", result)
}

func TestExtractCodexText_ObjectVariants(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"payload":{"text":"nested answer"}}`)
	result, ok := extractCodexText(raw)
	require.True(t, ok)
	assert.Equal(t, "nested answer", result)
}

func TestExtractCodexText_InvalidObject_ReturnsFalse(t *testing.T) {
	t.Parallel()

	result, ok := extractCodexText(json.RawMessage(`{`))
	assert.False(t, ok)
	assert.Empty(t, result)
}

func TestCreateCodexLastMessageFile_CreatesAndCleansUp(t *testing.T) {
	t.Parallel()

	path, cleanup, err := createCodexLastMessageFile()
	require.NoError(t, err)

	_, statErr := os.Stat(path)
	require.NoError(t, statErr)

	cleanup()

	_, statErr = os.Stat(path)
	require.Error(t, statErr)
	assert.True(t, os.IsNotExist(statErr))
}
