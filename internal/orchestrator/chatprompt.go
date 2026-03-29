package orchestrator

import (
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

func buildChatPrompt(role agent.Role, channel string, recentLines, beliefs []string, roster map[agent.Role]string, voiceText string) string {
	var builder strings.Builder

	name := roster[role]
	if name == "" {
		name = string(role)
	}

	builder.WriteString(fmt.Sprintf("You ARE %s. You are %s. You are IN a Slack conversation right now, posting as yourself. ", name, roleDescription(role)))
	builder.WriteString("Do NOT describe yourself, your knowledge, or your capabilities. Do NOT list what you know. Do NOT ask what to do. Just talk.")
	builder.WriteString("\nJames is the CEO. When he speaks, respond helpfully.")

	if len(beliefs) > 0 {
		builder.WriteString("\n\nThings you believe from experience: ")
		builder.WriteString(strings.Join(beliefs, "; "))
		builder.WriteString(".")
	}

	writeRoster(&builder, role, roster)

	fmt.Fprintf(&builder, "\n\nConversation in #%s:\n", channel)

	if len(recentLines) == 0 {
		builder.WriteString("(quiet — nothing recent)\n")
	}

	for _, line := range recentLines {
		fmt.Fprintf(&builder, "> %s\n", line)
	}

	lastMessage := ""
	if len(recentLines) > 0 {
		lastMessage = recentLines[len(recentLines)-1]
	}

	builder.WriteString("\n")
	builder.WriteString(channelInstruction(channel))

	if lastMessage != "" {
		fmt.Fprintf(&builder, " The most recent message is: \"%s\" — engage with it directly.", lastMessage)
	}

	// Voice is the LAST instruction before the format rules — this is what
	// the model pays most attention to. It shapes the actual response.
	if voiceText != "" {
		builder.WriteString("\n\nYou talk like this — match this voice exactly:\n")
		builder.WriteString(voiceText)
	}

	builder.WriteString("\n\nRules: 1-3 sentences. ONLY what you'd type in Slack.")
	builder.WriteString("\nThis is CHAT ONLY. You cannot run commands, merge PRs, deploy, or take any action from here. NEVER say 'I'm merging', 'I'll fix that', or 'on it' — you are just commenting, not doing.")
	builder.WriteString("\nNEVER list your capabilities, knowledge, or preferences. NEVER ask 'what do you want me to do'. NEVER describe the project. NEVER say 'I've read the CLAUDE.md' or list what you know.")
	builder.WriteString("\nIf someone made a valid point, acknowledge it. If you changed your mind, say so. Don't just repeat your position — build on what was said.")
	builder.WriteString("\nSlack formatting: *bold* not **bold**. No markdown headers.")
	builder.WriteString("\nIf you have nothing to add: PASS")

	return builder.String()
}

func roleDescription(role agent.Role) string {
	switch role {
	case agent.RolePM:
		return "the PM — you keep the team focused and unblocked"
	case agent.RoleTechLead:
		return "the tech lead — you think in systems and care about architecture"
	case agent.RoleEngineer1:
		return "an engineer — thorough, defensive, backend-leaning"
	case agent.RoleEngineer2:
		return "an engineer — fast, pragmatic, frontend-leaning"
	case agent.RoleEngineer3:
		return "an engineer — architectural, infra and DX focused"
	case agent.RoleReviewer:
		return "the reviewer — you catch bugs and ensure quality"
	case agent.RoleDesigner:
		return "the designer — you think from the user's perspective"
	}
	return string(role)
}

func writeRoster(builder *strings.Builder, self agent.Role, roster map[agent.Role]string) {
	if len(roster) == 0 {
		return
	}

	builder.WriteString("\n\nYour team: ")
	rosterParts := make([]string, 0, len(roster))
	for role, name := range roster {
		if role != self {
			rosterParts = append(rosterParts, fmt.Sprintf("%s (%s)", name, roleTitle(role)))
		}
	}
	builder.WriteString(strings.Join(rosterParts, ", "))
	builder.WriteString(". Use their names, not role IDs.")
}

func roleTitle(role agent.Role) string {
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

func channelInstruction(channel string) string {
	if channel == "chitchat" {
		return "This is the casual channel — talk about anything except work. Music, food, hot takes, weekend plans, something funny, a random thought. Be yourself."
	}
	return "Respond from YOUR perspective. If you disagree, say so. If you see a risk others missed, call it out. Don't just agree — add value or challenge."
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
