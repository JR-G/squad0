package orchestrator

import (
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

const chitchatChannel = "chitchat"

func buildChatPrompt(role agent.Role, channel string, recentLines, _ []string, roster map[agent.Role]string, _ string) string {
	var builder strings.Builder

	name := roster[role]
	if name == "" {
		name = string(role)
	}

	// Minimal prompt — identity is in CLAUDE.md, not here.
	// The user message is just context + instruction.
	fmt.Fprintf(&builder, "#%s:\n", channel)

	for _, line := range recentLines {
		fmt.Fprintf(&builder, "> %s\n", line)
	}

	if len(recentLines) == 0 {
		builder.WriteString("(quiet)\n")
	}

	builder.WriteString("\n")

	builder.WriteString(replyInstruction(name, channel))
	return builder.String()
}

// Kept for tests and other callers that reference these.

func roleDescription(role agent.Role) string {
	switch role {
	case agent.RolePM:
		return "PM"
	case agent.RoleTechLead:
		return "Tech Lead"
	case agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3:
		return "Engineer"
	case agent.RoleReviewer:
		return "Reviewer"
	case agent.RoleDesigner:
		return "Designer"
	}
	return string(role)
}

func roleTitle(role agent.Role) string {
	return roleDescription(role)
}

func replyInstruction(name, channel string) string {
	if channel == chitchatChannel {
		return fmt.Sprintf("Reply as %s — casual, not about work. Or PASS.", name)
	}
	return fmt.Sprintf("Reply as %s (1-3 sentences, Slack formatting, or PASS):", name)
}

func channelInstruction(channel string) string {
	if channel == chitchatChannel {
		return "casual"
	}
	return "work"
}

// ContainsQuestionForTest exports containsQuestion for testing.
func ContainsQuestionForTest(text string) bool {
	return containsQuestion(text)
}

// containsQuestion returns true if the text contains a question mark.
func containsQuestion(text string) bool {
	return strings.Contains(text, "?")
}

func containsPass(text string) bool {
	upper := strings.ToUpper(text)
	return strings.Contains(upper, "PASS")
}

// ChannelInstructionForTest exports channelInstruction for testing.
func ChannelInstructionForTest(channel string) string {
	return channelInstruction(channel)
}

// RoleDescriptionForTest exports roleDescription for testing.
func RoleDescriptionForTest(role agent.Role) string {
	return roleDescription(role)
}

// RoleTitleForTest exports roleTitle for testing.
func RoleTitleForTest(role agent.Role) string {
	return roleTitle(role)
}
