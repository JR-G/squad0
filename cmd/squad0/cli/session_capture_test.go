package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractLastResponse_ResultType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := `{"type":"user","content":"hello"}
{"type":"assistant","message":{"content":[{"type":"text","text":"thinking..."}]}}
{"type":"result","result":"the final answer"}
`
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o644))

	response := extractLastResponse(path)
	assert.Equal(t, "the final answer", response)
}

func TestExtractLastResponse_AssistantType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := `{"type":"user","content":"hello"}
{"type":"assistant","message":{"content":[{"type":"text","text":"assistant response"}]}}
`
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o644))

	response := extractLastResponse(path)
	assert.Equal(t, "assistant response", response)
}

func TestExtractLastResponse_EmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o644))

	response := extractLastResponse(path)
	assert.Empty(t, response)
}

func TestExtractLastResponse_MissingFile(t *testing.T) {
	t.Parallel()

	response := extractLastResponse("/nonexistent/path")
	assert.Empty(t, response)
}

func TestWriteToOutbox_CreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, writeToOutbox("engineer-1", dir, "test response"))

	data, err := os.ReadFile(filepath.Join(dir, "outbox", "engineer-1", "latest-response.json"))
	require.NoError(t, err)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "test response", parsed["response"])
}

func TestParseTranscriptLine_ResultType(t *testing.T) {
	t.Parallel()

	line := `{"type":"result","result":"done"}`
	assert.Equal(t, "done", parseTranscriptLine(line))
}

func TestParseTranscriptLine_InvalidJSON(t *testing.T) {
	t.Parallel()

	assert.Empty(t, parseTranscriptLine("not json"))
}

func TestParseTranscriptLine_NoContent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, parseTranscriptLine(`{"type":"user","content":"hello"}`))
}
