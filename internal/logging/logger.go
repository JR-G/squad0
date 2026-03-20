package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger writes structured JSON log entries to daily log files in the
// configured directory.
type Logger struct {
	mu          sync.Mutex
	logDir      string
	current     *os.File
	currentDate string
	slogger     *slog.Logger
}

// NewLogger creates a Logger that writes to daily files in the given
// directory. The directory is created if it does not exist.
func NewLogger(logDir string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}

	logger := &Logger{logDir: logDir}

	if err := logger.rotateIfNeeded(); err != nil {
		return nil, fmt.Errorf("opening initial log file: %w", err)
	}

	return logger, nil
}

// Info logs an informational message.
func (logger *Logger) Info(agentName, action, detail string) {
	logger.log(slog.LevelInfo, agentName, action, detail)
}

// Warn logs a warning message.
func (logger *Logger) Warn(agentName, action, detail string) {
	logger.log(slog.LevelWarn, agentName, action, detail)
}

// Error logs an error message.
func (logger *Logger) Error(agentName, action, detail string) {
	logger.log(slog.LevelError, agentName, action, detail)
}

// Close closes the current log file.
func (logger *Logger) Close() error {
	logger.mu.Lock()
	defer logger.mu.Unlock()

	if logger.current == nil {
		return nil
	}

	err := logger.current.Close()
	logger.current = nil
	return err
}

func (logger *Logger) log(level slog.Level, agentName, action, detail string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()

	_ = logger.rotateIfNeeded()

	logger.slogger.Log(context.Background(), level, action,
		"agent", agentName,
		"detail", detail,
	)
}

func (logger *Logger) rotateIfNeeded() error {
	today := time.Now().Format("2006-01-02")

	if today == logger.currentDate && logger.current != nil {
		return nil
	}

	if logger.current != nil {
		_ = logger.current.Close()
	}

	filename := filepath.Join(logger.logDir, today+".jsonl")
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening log file %s: %w", filename, err)
	}

	logger.current = file
	logger.currentDate = today
	logger.slogger = slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	return nil
}

// LogFilePath returns the path to the current day's log file.
func (logger *Logger) LogFilePath() string {
	logger.mu.Lock()
	defer logger.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	return filepath.Join(logger.logDir, today+".jsonl")
}
