package agent

import (
	"context"
	"encoding/json"
	"log"
	"strings"
)

// rateLimitSignals are strings in Claude CLI output that indicate rate limiting.
var rateLimitSignals = []string{
	"rate limit",
	"rate_limit",
	"too many requests",
	"429",
	"overloaded",
	"capacity",
}

// FallbackRunner tries Claude CLI first. If rate limited, retries
// the same prompt with Codex CLI. Transparent to callers.
type FallbackRunner struct {
	primary    ProcessRunner
	codexModel string
}

// NewFallbackRunner creates a runner that falls back to Codex on
// Claude rate limits.
func NewFallbackRunner(primary ProcessRunner, codexModel string) *FallbackRunner {
	return &FallbackRunner{primary: primary, codexModel: codexModel}
}

// Run tries the primary runner. If it fails with a rate limit signal,
// retries with Codex CLI.
func (runner *FallbackRunner) Run(ctx context.Context, stdin, workingDir, name string, args ...string) ([]byte, error) {
	output, err := runner.primary.Run(ctx, stdin, workingDir, name, args...)
	if err == nil {
		return output, nil
	}

	if !IsRateLimited(string(output), err) {
		return output, err
	}

	log.Printf("fallback: Claude rate limited, switching to Codex CLI")
	codexArgs := BuildCodexArgs(stdin, workingDir, runner.codexModel)
	return runner.primary.Run(ctx, "", workingDir, "codex", codexArgs...)
}

// IsRateLimited returns true if the output or error contains signals
// indicating Claude API rate limiting (429, capacity, overloaded).
func IsRateLimited(output string, err error) bool {
	lower := strings.ToLower(output + " " + err.Error())
	for _, signal := range rateLimitSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

// BuildCodexArgs constructs the argument list for `codex exec`.
func BuildCodexArgs(prompt, workingDir, model string) []string {
	args := []string{
		"exec",
		"--json",
		"--dangerously-bypass-approvals-and-sandbox",
	}

	if model != "" && model != "auto" {
		args = append(args, "-m", model)
	}

	if workingDir != "" {
		args = append(args, "-C", workingDir)
	}

	args = append(args, prompt)
	return args
}

// ParseCodexOutput extracts a transcript from Codex CLI's JSONL output.
func ParseCodexOutput(raw string) string {
	var lastContent string
	var lastPlain string

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if isCodexPlainMetaLine(line) {
			continue
		}

		content, ok := extractCodexContent(line)
		if content != "" {
			lastContent = content
			continue
		}

		if !ok {
			lastPlain = line
		}
	}

	if lastContent != "" {
		return lastContent
	}
	return lastPlain
}

func extractCodexContent(line string) (string, bool) {
	var msg struct {
		Type    string `json:"type"`
		Content string `json:"content"`
		Message string `json:"message"`
		Result  string `json:"result"`
	}

	if json.Unmarshal([]byte(line), &msg) != nil {
		return "", false
	}

	if isCodexMetaEvent(msg.Type) {
		return "", true
	}

	if msg.Content != "" {
		return msg.Content, true
	}
	if msg.Message != "" {
		return msg.Message, true
	}
	if msg.Result != "" {
		return msg.Result, true
	}
	return "", true
}

func isCodexMetaEvent(eventType string) bool {
	switch eventType {
	case "turn.completed", "response.completed", "thread.started", "turn.started", "turn.updated", "item.started", "item.updated", "token.count", "usage", "event":
		return true
	default:
		return false
	}
}

func isCodexPlainMetaLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	switch {
	case lower == "":
		return true
	case strings.HasPrefix(lower, "reading additional input from stdin"):
		return true
	default:
		return false
	}
}

// IsRateLimitedForTest exports IsRateLimited for backwards-compatible
// test callers.
func IsRateLimitedForTest(output string, err error) bool {
	return IsRateLimited(output, err)
}
