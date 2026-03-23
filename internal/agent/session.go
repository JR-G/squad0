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
	SubType string          `json:"subtype,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	Result  string          `json:"result,omitempty"`
	Message json.RawMessage `json:"message,omitempty"`
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
	MCPConfigPath   string
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
		"--verbose",
		"--dangerously-skip-permissions",
	}

	if cfg.MaxTurnDuration > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", cfg.MaxTurnDuration))
	}

	if cfg.MCPConfigPath != "" {
		args = append(args, "--mcp-config", cfg.MCPConfigPath)
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
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if messages[idx].Type == "result" && messages[idx].Result != "" {
			return messages[idx].Result
		}
	}

	for _, msg := range messages {
		if msg.Type != "assistant" {
			continue
		}

		text := extractAssistantText(msg)
		if text != "" {
			return text
		}
	}

	return ""
}

func extractAssistantText(msg StreamMessage) string {
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(msg.Message, &parsed); err != nil {
		return ""
	}

	var builder strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			builder.WriteString(block.Text)
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
