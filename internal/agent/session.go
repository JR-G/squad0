package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ProcessRunner executes a command with stdin and captures stdout.
// This interface exists to enable testing without spawning real processes.
type ProcessRunner interface {
	Run(ctx context.Context, stdin string, name string, args ...string) ([]byte, error)
}

// ExecProcessRunner implements ProcessRunner using os/exec.
type ExecProcessRunner struct{}

// Run executes the named command, pipes stdin, and returns combined output.
func (runner ExecProcessRunner) Run(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("running %s: %w", name, err)
	}

	return output, nil
}

// StreamMessage represents a single line of Claude Code's stream-json output.
type StreamMessage struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// SessionResult holds the outcome of a completed agent session.
type SessionResult struct {
	RawOutput  string
	Messages   []StreamMessage
	ExitCode   int
	Transcript string
}

// SessionConfig holds the parameters needed to run an agent session.
type SessionConfig struct {
	Role            Role
	Model           string
	Prompt          string
	WorkingDir      string
	MaxTurnDuration int
}

// Session manages a single Claude Code agent session.
type Session struct {
	runner ProcessRunner
}

// NewSession creates a Session with the given process runner.
func NewSession(runner ProcessRunner) *Session {
	return &Session{runner: runner}
}

// Run executes a Claude Code session with the given configuration and
// returns the parsed result.
func (session *Session) Run(ctx context.Context, cfg SessionConfig) (SessionResult, error) {
	args := buildArgs(cfg)

	output, err := session.runner.Run(ctx, cfg.Prompt, "claude", args...)

	var result SessionResult
	result.RawOutput = string(output)

	if err != nil {
		exitErr := ExtractExitError(err)
		result.ExitCode = exitErr
		result.Messages = parseStreamOutput(result.RawOutput)
		result.Transcript = extractTranscript(result.Messages)
		return result, fmt.Errorf("claude session failed (exit %d): %w", result.ExitCode, err)
	}

	result.Messages = parseStreamOutput(result.RawOutput)
	result.Transcript = extractTranscript(result.Messages)

	return result, nil
}

func buildArgs(cfg SessionConfig) []string {
	args := []string{
		"-p",
		"--model", cfg.Model,
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
	}

	if cfg.MaxTurnDuration > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", cfg.MaxTurnDuration))
	}

	return args
}

func parseStreamOutput(raw string) []StreamMessage {
	var messages []StreamMessage
	scanner := bufio.NewScanner(strings.NewReader(raw))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg StreamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	return messages
}

func extractTranscript(messages []StreamMessage) string {
	var builder strings.Builder

	for _, msg := range messages {
		switch msg.Type {
		case "assistant":
			var content string
			if err := json.Unmarshal(msg.Content, &content); err == nil {
				builder.WriteString(content)
				builder.WriteString("\n")
			}
		case "result":
			var content string
			if err := json.Unmarshal(msg.Content, &content); err == nil {
				builder.WriteString(content)
				builder.WriteString("\n")
			}
		}
	}

	return builder.String()
}

// ExtractExitError returns the exit code from an exec.ExitError, or 1
// if the error is not an exit error.
func ExtractExitError(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}
