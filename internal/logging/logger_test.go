package logging_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger_CreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "logs")
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewLogger_InvalidPath_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := logging.NewLogger("/dev/null/impossible")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating log directory")
}

func TestLogger_Info_WritesJSONLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	logger.Info("engineer-1", "session_start", "starting work on SQ-42")
	_ = logger.Close()

	content := readLogFile(t, dir)
	assert.Contains(t, content, "engineer-1")
	assert.Contains(t, content, "session_start")
	assert.Contains(t, content, "SQ-42")

	assertValidJSON(t, content)
}

func TestLogger_Warn_WritesWarnLevel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	logger.Warn("pm", "rate_limit", "approaching Slack API limit")
	_ = logger.Close()

	content := readLogFile(t, dir)
	assert.Contains(t, content, "WARN")
}

func TestLogger_Error_WritesErrorLevel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	logger.Error("reviewer", "session_failed", "context exhausted")
	_ = logger.Close()

	content := readLogFile(t, dir)
	assert.Contains(t, content, "ERROR")
}

func TestLogger_MultipleEntries_AllWritten(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	logger.Info("engineer-1", "start", "first")
	logger.Info("engineer-2", "start", "second")
	logger.Warn("pm", "check", "third")
	_ = logger.Close()

	content := readLogFile(t, dir)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	assert.Len(t, lines, 3)
}

func TestLogger_LogFilePath_ReturnsCurrentDayFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	path := logger.LogFilePath()
	today := time.Now().Format("2006-01-02")

	assert.Contains(t, path, today)
	assert.Contains(t, path, ".jsonl")
}

func TestLogger_Close_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger, err := logging.NewLogger(dir)
	require.NoError(t, err)

	require.NoError(t, logger.Close())
	require.NoError(t, logger.Close())
}

func readLogFile(t *testing.T, dir string) string {
	t.Helper()
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, today+".jsonl")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func assertValidJSON(t *testing.T, content string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if line == "" {
			continue
		}
		var parsed map[string]interface{}
		err := json.Unmarshal([]byte(line), &parsed)
		assert.NoError(t, err, "invalid JSON line: %s", line)
	}
}
