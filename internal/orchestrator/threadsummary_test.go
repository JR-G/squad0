package orchestrator_test

import (
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestSummariseThread_ShortThread_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	lines := []string{
		"Alice: Hello",
		"Bob: Hi there",
	}

	result := orchestrator.SummariseThread(lines, 5)

	assert.Empty(t, result, "short thread should return empty summary")
}

func TestSummariseThread_LongThread_Condenses(t *testing.T) {
	t.Parallel()

	lines := []string{
		"Alice: First message",
		"Bob: Second message",
		"Charlie: Third message",
		"Alice: Fourth message",
		"Bob: Fifth message",
		"Charlie: Sixth message",
		"Alice: Seventh message",
		"Bob: Eighth message",
		"Charlie: Ninth message",
		"Alice: Tenth message",
	}

	result := orchestrator.SummariseThread(lines, 4)

	assert.NotEmpty(t, result)
	// Should mention skipped count.
	assert.Contains(t, result, "6 earlier messages")
	// Should mention speakers from the full thread.
	assert.Contains(t, result, "Alice")
	assert.Contains(t, result, "Bob")
	assert.Contains(t, result, "Charlie")
	// Tail lines should be quoted.
	assert.Contains(t, result, "> Alice: Seventh message")
}

func TestSummariseThread_UnansweredQuestion_Flagged(t *testing.T) {
	t.Parallel()

	lines := []string{
		"Alice: I pushed the fix.",
		"Bob: Looks good.",
		"Charlie: Another message.",
		"Alice: More context here.",
		"Bob: Yet another line.",
		"Charlie: Should we add retry logic?",
	}

	result := orchestrator.SummariseThread(lines, 3)

	assert.NotEmpty(t, result)
	assert.Contains(t, result, "unanswered question")
}

func TestSummariseThread_NoUnansweredQuestion_NotFlagged(t *testing.T) {
	t.Parallel()

	lines := []string{
		"Alice: What should we do?",
		"Bob: Let's add retry logic.",
		"Charlie: Agreed.",
		"Alice: Sounds good.",
		"Bob: I will start on it.",
		"Charlie: Done.",
	}

	result := orchestrator.SummariseThread(lines, 3)

	assert.NotEmpty(t, result)
	assert.NotContains(t, result, "unanswered question",
		"last line has no question mark so no unanswered question")
}

func TestBuildChatPromptWithSummary_NoSummary_ReturnsBase(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Kira",
	}
	recent := []string{"Kira: Working on auth module."}
	beliefs := []string{"Auth module is fragile."}

	result := orchestrator.BuildChatPromptWithSummary(
		agent.RoleEngineer1, "#engineering", recent, beliefs, roster, "Be concise.", "",
	)

	assert.NotEmpty(t, result)
	assert.False(t, strings.HasPrefix(result, "Thread context:"),
		"should not prepend thread context when summary is empty")
}

func TestBuildChatPromptWithSummary_WithSummary_PrependsSummary(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Kira",
	}
	recent := []string{"Kira: Working on auth module."}
	beliefs := []string{"Auth module is fragile."}
	summary := "(3 earlier messages from Alice, Bob)\n> Alice: Context.\n"

	result := orchestrator.BuildChatPromptWithSummary(
		agent.RoleEngineer1, "#engineering", recent, beliefs, roster, "Be concise.", summary,
	)

	assert.True(t, strings.HasPrefix(result, "Thread context:"),
		"should prepend thread context when summary is provided")
	assert.Contains(t, result, summary)
}
