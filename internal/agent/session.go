package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// ProcessRunner executes a command with stdin and captures stdout.
// This interface exists to enable testing without spawning real processes.
type ProcessRunner interface {
	Run(ctx context.Context, stdin, workingDir, name string, args ...string) ([]byte, error)
}

// ExecProcessRunner implements ProcessRunner using os/exec.
type ExecProcessRunner struct {
	// ExtraEnv holds additional environment variables set on every
	// process. These override inherited env vars.
	ExtraEnv map[string]string
}

// Run executes the named command, pipes stdin, and returns combined output.
// When workingDir is non-empty the process runs in that directory.
func (runner ExecProcessRunner) Run(ctx context.Context, stdin, workingDir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)

	if workingDir != "" {
		cmd.Dir = workingDir
	}

	if len(runner.ExtraEnv) > 0 {
		cmd.Env = os.Environ()
		for key, val := range runner.ExtraEnv {
			cmd.Env = append(cmd.Env, key+"="+val)
		}
	}

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
	Env             map[string]string // Extra environment variables for the process.
}

// Session manages a single Claude Code agent session.
type Session struct {
	runner     ProcessRunner
	codexModel string // If set, enables Codex CLI fallback on rate limit.
}

// NewSession creates a Session with the given process runner.
func NewSession(runner ProcessRunner) *Session {
	return &Session{runner: runner}
}

// SetCodexFallback enables automatic fallback to Codex CLI when
// Claude is rate limited. The codexModel specifies which model
// to use (e.g. "o3", "gpt-4.1").
func (session *Session) SetCodexFallback(model string) {
	session.codexModel = model
}

// Run executes a Claude Code session with the given configuration and
// returns the parsed result. If Claude is rate limited and Codex
// fallback is configured, retries with Codex CLI automatically.
func (session *Session) Run(ctx context.Context, cfg SessionConfig) (SessionResult, error) {
	restoreEnv := applyEnv(cfg.Env)
	defer restoreEnv()

	args := buildArgs(cfg)
	output, err := session.runner.Run(ctx, cfg.Prompt, cfg.WorkingDir, "claude", args...)

	result := parseClaudeResult(output, err)

	// If rate limited and Codex fallback is configured, retry.
	if err != nil && session.codexModel != "" && isRateLimited(result.RawOutput, err) {
		log.Printf("rate limited on Claude, falling back to Codex (%s)", session.codexModel)
		return session.runCodex(ctx, cfg)
	}

	if err != nil {
		return result, fmt.Errorf("claude session failed (exit %d): %w", result.ExitCode, err)
	}

	return result, nil
}

func parseClaudeResult(output []byte, err error) SessionResult {
	var result SessionResult
	result.RawOutput = string(output)

	if err != nil {
		result.ExitCode = ExtractExitError(err)
	}

	result.Messages = parseStreamOutput(result.RawOutput)
	result.Transcript = extractTranscript(result.Messages)
	return result
}

func (session *Session) runCodex(ctx context.Context, cfg SessionConfig) (SessionResult, error) {
	codexArgs := BuildCodexArgs(cfg.Prompt, cfg.WorkingDir, session.codexModel)
	output, err := session.runner.Run(ctx, "", cfg.WorkingDir, "codex", codexArgs...)

	var result SessionResult
	result.RawOutput = string(output)

	if err != nil {
		result.ExitCode = ExtractExitError(err)
		result.Transcript = ParseCodexOutput(result.RawOutput)
		return result, fmt.Errorf("codex session failed (exit %d): %w", result.ExitCode, err)
	}

	result.Transcript = ParseCodexOutput(result.RawOutput)
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

// applyEnv sets environment variables and returns a function that
// restores the previous values.
func applyEnv(env map[string]string) func() {
	if len(env) == 0 {
		return func() {}
	}

	previous := make(map[string]string, len(env))
	for key, val := range env {
		previous[key] = os.Getenv(key)
		_ = os.Setenv(key, val)
	}

	return func() {
		for key, oldVal := range previous {
			restoreEnvVar(key, oldVal)
		}
	}
}

func restoreEnvVar(key, oldVal string) {
	if oldVal == "" {
		_ = os.Unsetenv(key)
		return
	}
	_ = os.Setenv(key, oldVal)
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
