package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SessionWriter captures full Claude Code session output to disk after
// each session ends.
type SessionWriter struct {
	baseDir string
}

// NewSessionWriter creates a SessionWriter that stores session transcripts
// under the given base directory, organised by agent name.
func NewSessionWriter(baseDir string) *SessionWriter {
	return &SessionWriter{baseDir: baseDir}
}

// WriteSession saves the raw session output to a timestamped file under
// data/sessions/{agent}/{timestamp}.txt. Returns the path of the written
// file.
func (writer *SessionWriter) WriteSession(agentName, output string) (string, error) {
	agentDir := filepath.Join(writer.baseDir, agentName)

	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return "", fmt.Errorf("creating session directory for %s: %w", agentName, err)
	}

	timestamp := time.Now().Format("2006-01-02T15-04-05.000000000")
	filename := timestamp + ".txt"
	path := filepath.Join(agentDir, filename)

	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return "", fmt.Errorf("writing session file for %s: %w", agentName, err)
	}

	return path, nil
}

// SessionDir returns the directory where sessions are stored for a given
// agent.
func (writer *SessionWriter) SessionDir(agentName string) string {
	return filepath.Join(writer.baseDir, agentName)
}
