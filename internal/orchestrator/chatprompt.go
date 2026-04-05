package orchestrator

import (
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

const chitchatChannel = "chitchat"

func buildChatPrompt(role agent.Role, channel string, recentLines, _ []string, roster map[agent.Role]string, voiceText string) string {
	var builder strings.Builder

	name := roster[role]
	if name == "" {
		name = string(role)
	}

	fmt.Fprintf(&builder, "#%s:\n", channel)

	for _, line := range recentLines {
		fmt.Fprintf(&builder, "> %s\n", line)
	}

	if len(recentLines) == 0 {
		builder.WriteString("(quiet)\n")
	}

	builder.WriteString("\n")

	// Voice reinforcement — the CLAUDE.md has the full voice description
	// but repeating key instructions in the prompt keeps it front of mind.
	if voiceText != "" {
		fmt.Fprintf(&builder, "Voice reminder: %s\n\n", voiceText)
	}

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

// ReplyInstructionForTest exports replyInstruction for testing.
func ReplyInstructionForTest(name, channel string) string {
	return replyInstruction(name, channel)
}

func replyInstruction(name, channel string) string {
	if channel == chitchatChannel {
		return fmt.Sprintf("You're %s, hanging out with your team. Say whatever's on your mind — an opinion, a reaction, something funny, a rant, a random thought. Talk like a real person on Slack with colleagues you like. 1-3 sentences.", name)
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
	trimmed := strings.TrimSpace(strings.ToUpper(text))
	// Match standalone "PASS" or responses that start with "PASS —"
	// (agent explicitly passing then explaining why).
	if trimmed == "PASS" || trimmed == "PASS." {
		return true
	}
	return strings.HasPrefix(trimmed, "PASS —") ||
		strings.HasPrefix(trimmed, "PASS -") ||
		strings.HasPrefix(trimmed, "PASS\n")
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
