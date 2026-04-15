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
		return fmt.Sprintf("You are %s. Say \"I\" — never refer to yourself by name or role. Say whatever's on your mind — an opinion, a reaction, something funny, a rant, a random thought. Talk like a real person on Slack with colleagues you like. 1-3 sentences.", name)
	}
	return fmt.Sprintf(
		"You are %s. Say \"I\" — never refer to yourself by name or role. 1-3 sentences, Slack formatting.\n\n"+
			"IMPORTANT: If you have nothing meaningful to add — no new information, no question, no concrete next step, no opinion that hasn't been said — respond with exactly the word PASS on its own line and nothing else. "+
			"Do NOT write \"nothing to add\", \"thread's locked\", \"I'm good\", \"waiting for PR\" or similar filler. Those are worse than silence. "+
			"PASS is correct and expected — it's how you stay quiet. Speak only when you have something real to contribute.",
		name)
}

// passSentinel is the exact token an agent emits when it has nothing
// to add. Case-insensitive match; anything surrounding it (quotes,
// punctuation, whitespace) is trimmed before comparison.
const passSentinel = "PASS"

// isPassResponse reports whether a chat response is a pass sentinel
// — the agent's way of saying "stay quiet." An empty string is NOT a
// pass; that is a generation failure that should retry. Handles a few
// common wrapping patterns the model produces even when told to emit
// exactly PASS: wrapping quotes, trailing punctuation, a leading
// "PASS." etc. Kept deliberately tight so real messages that happen
// to contain the word "pass" (e.g. "I'll pass on that idea") still
// post normally.
func isPassResponse(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	trimmed = strings.Trim(trimmed, "\"'`.* \t\n")
	return strings.EqualFold(trimmed, passSentinel)
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

// IsPassResponseForTest exports isPassResponse for testing.
func IsPassResponseForTest(text string) bool {
	return isPassResponse(text)
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
