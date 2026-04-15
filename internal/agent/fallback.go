package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
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
// When outputLastMessagePath is provided, Codex also writes the final
// assistant message to that file so callers can recover it even when
// the JSONL stream omits the final text.
func BuildCodexArgs(prompt, workingDir, model string, outputLastMessagePath ...string) []string {
	// Override interactive Codex defaults that break unattended fallback:
	// some sessions run outside git repos, and squad0 should keep fallback
	// reasoning effort at a moderate cost level.
	args := []string{
		"exec",
		"--json",
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
		"-c", `model_reasoning_effort="medium"`,
	}

	if model != "" && model != "auto" {
		args = append(args, "-m", model)
	}

	if workingDir != "" {
		args = append(args, "-C", workingDir)
	}

	if len(outputLastMessagePath) > 0 && outputLastMessagePath[0] != "" {
		args = append(args, "-o", outputLastMessagePath[0])
	}

	args = append(args, prompt)
	return args
}

// ResolveCodexTranscript returns the best transcript available from Codex.
// It prefers the JSONL stdout stream and falls back to the explicit
// last-message file when stdout does not include the final assistant text.
func ResolveCodexTranscript(rawOutput, outputLastMessagePath string) string {
	if transcript := ParseCodexOutput(rawOutput); transcript != "" {
		return transcript
	}
	if outputLastMessagePath == "" {
		return ""
	}

	data, err := os.ReadFile(outputLastMessagePath)
	if err != nil {
		return ""
	}
	return ParseCodexOutput(string(data))
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
		if transcript := normalizeCodexTranscript(content); transcript != "" {
			lastContent = transcript
			continue
		}

		if !ok {
			lastPlain = normalizeCodexTranscript(line)
		}
	}

	if lastContent != "" {
		return lastContent
	}
	return normalizeCodexTranscript(lastPlain)
}

func extractCodexContent(line string) (string, bool) {
	return extractCodexContentBytes([]byte(line))
}

func extractCodexContentBytes(data []byte) (string, bool) {
	var msg struct {
		Type             string          `json:"type"`
		Role             string          `json:"role"`
		Content          json.RawMessage `json:"content"`
		Message          json.RawMessage `json:"message"`
		Result           json.RawMessage `json:"result"`
		Payload          json.RawMessage `json:"payload"`
		LastAgentMessage string          `json:"last_agent_message"`
	}

	if json.Unmarshal(data, &msg) != nil {
		return "", false
	}

	if content := normalizeCodexTranscript(msg.LastAgentMessage); content != "" {
		return content, true
	}

	if content, ok := extractCodexContentBytes(msg.Payload); ok {
		return content, true
	}

	if isCodexMetaEvent(msg.Type) {
		return "", true
	}

	if msg.Role != "" && msg.Role != streamRoleAssistant {
		return "", true
	}

	for _, field := range []json.RawMessage{msg.Content, msg.Message, msg.Result} {
		if content, ok := extractCodexText(field); ok {
			return content, true
		}
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

func extractCodexText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", false
	}

	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, true
	}

	var items []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
		Message string `json:"message"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(raw, &items) == nil {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			switch {
			case normalizeCodexTranscript(item.Text) != "":
				parts = append(parts, normalizeCodexTranscript(item.Text))
			case normalizeCodexTranscript(item.Content) != "":
				parts = append(parts, normalizeCodexTranscript(item.Content))
			case normalizeCodexTranscript(item.Message) != "":
				parts = append(parts, normalizeCodexTranscript(item.Message))
			case normalizeCodexTranscript(item.Result) != "":
				parts = append(parts, normalizeCodexTranscript(item.Result))
			}
		}
		return strings.Join(parts, "\n"), true
	}

	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return "", false
	}

	for _, key := range []string{"last_agent_message", "text", "content", "message", "result", "payload"} {
		value, ok := obj[key]
		if !ok {
			continue
		}
		if text, ok := extractCodexText(value); ok {
			return text, true
		}
	}

	return "", true
}

func normalizeCodexTranscript(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.EqualFold(trimmed, "null") {
		return ""
	}
	return trimmed
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
