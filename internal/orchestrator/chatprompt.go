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

	builder.WriteString(fmt.Sprintf("Your name is %s. You are %s. ", name, roleDescription(role)))
	builder.WriteString("James is the CEO — he built the team and has final say. When he speaks, pay attention and respond helpfully.")

	if voiceText != "" {
		builder.WriteString("\n\n## Your Voice\n")
		builder.WriteString(voiceText)
	}

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

	builder.WriteString("\nRespond to this conversation. You're talking to real teammates.")

	if lastMessage != "" {
		fmt.Fprintf(&builder, " The most recent message is: \"%s\" — engage with it directly.", lastMessage)
	}

	builder.WriteString(" Use people's names. Ask follow-up questions. Disagree if you disagree. Build on their ideas. Be yourself — your tone, your perspective, your opinions.")
	builder.WriteString("\n\nKeep it to 1-3 sentences. Respond with ONLY what you'd type in Slack.")
	builder.WriteString("\nNEVER include meta-commentary, parenthetical notes, stage directions, or alternatives.")
	builder.WriteString("\nNEVER break character or mention being an AI.")
	builder.WriteString("\nNEVER describe the project, your tech stack, or your role — just talk like a person.")
	builder.WriteString("\nIf you genuinely have nothing to add, respond with exactly: PASS")

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

// ContainsQuestionForTest exports containsQuestion for testing.
func ContainsQuestionForTest(text string) bool {
	return containsQuestion(text)
}

// containsQuestion returns true if the text ends with a question mark
// or contains common question patterns.
func containsQuestion(text string) bool {
	return strings.Contains(text, "?")
}

func containsPass(text string) bool {
	upper := strings.ToUpper(text)
	return strings.Contains(upper, "PASS")
}
