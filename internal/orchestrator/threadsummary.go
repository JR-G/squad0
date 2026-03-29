package orchestrator

import (
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

const summariseThreshold = 8

// SummariseThread condenses a long thread into a compact context
// string. Returns empty if the thread is short enough to include
// verbatim. Deterministic — no LLM calls.
func SummariseThread(recentLines []string, maxLines int) string {
	if len(recentLines) <= maxLines {
		return ""
	}

	speakers := uniqueSpeakers(recentLines)
	tail := recentLines[len(recentLines)-maxLines:]
	skipped := len(recentLines) - maxLines

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("(%d earlier messages from %s)\n", skipped, strings.Join(speakers, ", ")))

	for _, line := range tail {
		fmt.Fprintf(&builder, "> %s\n", line)
	}

	if hasUnansweredQuestion(tail) {
		builder.WriteString("^ There's an unanswered question above.\n")
	}

	return builder.String()
}

func uniqueSpeakers(lines []string) []string {
	seen := make(map[string]bool)
	speakers := make([]string, 0, 4)

	for _, line := range lines {
		idx := strings.Index(line, ":")
		if idx <= 0 || idx > 30 {
			continue
		}
		name := line[:idx]
		if seen[name] {
			continue
		}
		seen[name] = true
		speakers = append(speakers, name)
	}

	return speakers
}

func hasUnansweredQuestion(lines []string) bool {
	if len(lines) == 0 {
		return false
	}

	last := lines[len(lines)-1]
	return strings.Contains(last, "?")
}

// BuildChatPromptWithSummary wraps buildChatPrompt and prepends thread
// context when the conversation is long enough to need summarisation.
func BuildChatPromptWithSummary(
	role agent.Role,
	channel string,
	recentLines, beliefs []string,
	roster map[agent.Role]string,
	voiceText string,
	threadSummary string,
) string {
	base := buildChatPrompt(role, channel, recentLines, beliefs, roster, voiceText)
	if threadSummary == "" {
		return base
	}

	return "Thread context:\n" + threadSummary + "\n" + base
}
