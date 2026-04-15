package agent

import (
	"bufio"
	"bytes"
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

type envProcessRunner struct {
	base ProcessRunner
	env  map[string]string
}

// Run executes the named command, pipes stdin, and returns stdout only.
// Stderr is logged but not mixed into the output — prevents CLI noise
// like "Reading additional input from stdin..." from corrupting transcripts.
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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if stderrStr := stderr.String(); stderrStr != "" {
		log.Printf("stderr from %s: %s", name, strings.TrimSpace(stderrStr))
	}

	if err != nil {
		// Include stderr in the error for debugging, but return
		// only stdout as the output for transcript parsing.
		return stdout.Bytes(), fmt.Errorf("running %s: %w", name, err)
	}

	return stdout.Bytes(), nil
}

func (runner envProcessRunner) Run(ctx context.Context, stdin, workingDir, name string, args ...string) ([]byte, error) {
	restoreEnv := applyEnv(runner.env)
	defer restoreEnv()
	return runner.base.Run(ctx, stdin, workingDir, name, args...)
}

// streamRoleAssistant is the role value on assistant messages in
// Claude Code's stream-json output.
const streamRoleAssistant = "assistant"

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
	SystemPrompt    string // Injected via --append-system-prompt. Used for persona anchoring.
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
	// Env vars are passed via the runner's ExtraEnv, not os.Setenv.
	// os.Setenv is process-global and leaks across concurrent sessions.
	runner := session.runnerWithEnv(cfg.Env)

	args := buildArgs(cfg)
	output, err := runner.Run(ctx, cfg.Prompt, cfg.WorkingDir, "claude", args...)

	result := parseClaudeResult(output, err)

	// Post-hoc visibility: log a one-line summary of what tools the
	// session actually called so operators can see work happening
	// without reading the ~/.claude jsonl transcript by hand. Only
	// fires when the session did at least one tool call — a plain
	// chat exchange (QuickChat) shouldn't spam the log.
	summary := SummariseToolCalls(result.Messages)
	if len(summary.Counts) > 0 {
		log.Printf("session %s finished: %s", cfg.Role, summary.Format())
	}

	// If rate limited and Codex fallback is configured, retry.
	if err != nil {
		rateLimited := IsRateLimited(result.RawOutput, err)
		log.Printf(
			"claude session failed for %s: rate_limited=%t fallback_configured=%t exit=%d err=%v output=%q",
			cfg.Role,
			rateLimited,
			session.codexModel != "",
			result.ExitCode,
			err,
			summarizeFailureOutput(result.RawOutput),
		)
	}

	if err != nil && session.codexModel != "" && IsRateLimited(result.RawOutput, err) {
		log.Printf("rate limited on Claude, falling back to Codex (%s)", session.codexModel)
		return session.runCodex(ctx, cfg, runner)
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

func (session *Session) runCodex(ctx context.Context, cfg SessionConfig, runner ProcessRunner) (SessionResult, error) {
	log.Printf("codex fallback started for %s: model=%s working_dir=%q", cfg.Role, session.codexModel, cfg.WorkingDir)

	lastMessageFile, cleanup, err := createCodexLastMessageFile()
	if err != nil {
		return SessionResult{}, fmt.Errorf("create codex last-message file: %w", err)
	}
	defer cleanup()

	codexArgs := BuildCodexArgs(cfg.Prompt, cfg.WorkingDir, session.codexModel, lastMessageFile)
	output, err := runner.Run(ctx, "", cfg.WorkingDir, "codex", codexArgs...)

	var result SessionResult
	result.RawOutput = string(output)

	if err != nil {
		result.ExitCode = ExtractExitError(err)
		result.Transcript = ResolveCodexTranscript(result.RawOutput, lastMessageFile)
		log.Printf(
			"codex fallback failed for %s: exit=%d err=%v output=%q",
			cfg.Role,
			result.ExitCode,
			err,
			summarizeFailureOutput(result.RawOutput),
		)
		return result, fmt.Errorf("codex session failed (exit %d): %w", result.ExitCode, err)
	}

	result.Transcript = ResolveCodexTranscript(result.RawOutput, lastMessageFile)
	if result.Transcript == "" {
		return result, fmt.Errorf("codex returned empty response")
	}
	log.Printf("codex fallback succeeded for %s", cfg.Role)
	return result, nil
}

func createCodexLastMessageFile() (path string, cleanup func(), err error) {
	file, err := os.CreateTemp("", "squad0-codex-last-message-*.txt")
	if err != nil {
		return "", nil, err
	}
	path = file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return "", nil, closeErr
	}
	cleanup = func() {
		_ = os.Remove(path)
	}
	return path, cleanup, nil
}

func (session *Session) runnerWithEnv(env map[string]string) ProcessRunner {
	if len(env) == 0 {
		return session.runner
	}

	switch runner := session.runner.(type) {
	case ExecProcessRunner:
		runner.ExtraEnv = mergeEnv(runner.ExtraEnv, env)
		return runner
	case *ExecProcessRunner:
		copyRunner := *runner
		copyRunner.ExtraEnv = mergeEnv(copyRunner.ExtraEnv, env)
		return &copyRunner
	default:
		return envProcessRunner{
			base: session.runner,
			env:  mergeEnv(nil, env),
		}
	}
}

func mergeEnv(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}

	merged := make(map[string]string, len(base)+len(extra))
	for key, val := range base {
		merged[key] = val
	}
	for key, val := range extra {
		merged[key] = val
	}
	return merged
}

func buildArgs(cfg SessionConfig) []string {
	args := []string{
		"-p",
		"--model", cfg.Model,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
	}

	if cfg.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", cfg.SystemPrompt)
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
		if msg.Type != streamRoleAssistant {
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

func summarizeFailureOutput(raw string) string {
	const maxLen = 400

	trimmed := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= maxLen {
		return trimmed
	}
	return trimmed[:maxLen] + "...(truncated)"
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

// SummarizeFailureOutputForTest exports summarizeFailureOutput for testing.
func SummarizeFailureOutputForTest(raw string) string {
	return summarizeFailureOutput(raw)
}

// ExtractAssistantTextForTest exports extractAssistantText for testing.
func ExtractAssistantTextForTest(msg StreamMessage) string {
	return extractAssistantText(msg)
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
