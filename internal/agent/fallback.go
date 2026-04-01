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

	if !isRateLimited(string(output), err) {
		return output, err
	}

	log.Printf("fallback: Claude rate limited, switching to Codex CLI")
	codexArgs := BuildCodexArgs(stdin, workingDir, runner.codexModel)
	return runner.primary.Run(ctx, "", workingDir, "codex", codexArgs...)
}

func isRateLimited(output string, err error) bool {
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

	if model != "" {
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

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		content := extractCodexContent(line)
		if content != "" {
			lastContent = content
			continue
		}

		lastContent = line
	}

	return lastContent
}

func extractCodexContent(line string) string {
	var msg struct {
		Type    string `json:"type"`
		Content string `json:"content"`
		Message string `json:"message"`
	}

	if json.Unmarshal([]byte(line), &msg) != nil {
		return ""
	}

	if msg.Content != "" {
		return msg.Content
	}
	return msg.Message
}

// IsRateLimitedForTest exports isRateLimited for testing.
func IsRateLimitedForTest(output string, err error) bool {
	return isRateLimited(output, err)
}
