package cli

import (
	"encoding/json"
	"fmt"
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

func TestRunSessionCapture_StopHookActive_ReturnsNil(t *testing.T) {
	// Not parallel — modifies os.Stdin.
	input := `{"session_id":"abc","transcript_path":"","stop_hook_active":true}`
	r, w, _ := os.Pipe()
	_, _ = w.WriteString(input)
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	err := runSessionCapture("engineer-1", t.TempDir())
	os.Stdin = old
	assert.NoError(t, err)
}

func TestRunSessionCapture_EmptyTranscript_ReturnsNil(t *testing.T) {
	input := `{"session_id":"abc","transcript_path":"","stop_hook_active":false}`
	r, w, _ := os.Pipe()
	_, _ = w.WriteString(input)
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	err := runSessionCapture("engineer-1", t.TempDir())
	os.Stdin = old
	assert.NoError(t, err)
}

func TestRunSessionCapture_WithTranscript_WritesOutbox(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	_ = os.WriteFile(transcriptPath, []byte(`{"type":"result","result":"the response"}`+"\n"), 0o644)

	input := `{"session_id":"abc","transcript_path":` + fmt.Sprintf("%q", transcriptPath) + `,"stop_hook_active":false}`
	r, w, _ := os.Pipe()
	_, _ = w.WriteString(input)
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	err := runSessionCapture("engineer-1", dir)
	os.Stdin = old
	assert.NoError(t, err)

	data, readErr := os.ReadFile(filepath.Join(dir, "outbox", "engineer-1", "latest-response.json"))
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "the response")
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

func TestWriteToOutbox_InvalidPath_ReturnsError(t *testing.T) {
	t.Parallel()

	err := writeToOutbox("test", "/dev/null", "response")
	assert.Error(t, err)
}

func TestParseTranscriptLine_NoContent(t *testing.T) {
	t.Parallel()

	assert.Empty(t, parseTranscriptLine(`{"type":"user","content":"hello"}`))
}
