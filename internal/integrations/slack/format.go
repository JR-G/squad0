package slack

import (
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
)

// FormatStatusForSlack renders agent check-ins as Slack-formatted text.
// Uses personas to show chosen names instead of role IDs.
func FormatStatusForSlack(checkIns []coordination.CheckIn, personas map[agent.Role]Persona) string {
	if len(checkIns) == 0 {
		return "No agents registered yet."
	}

	var builder strings.Builder
	builder.WriteString("*Squad Status*\n\n")

	for _, checkIn := range checkIns {
		name := displayNameForStatus(checkIn.Agent, personas)
		status := formatSlackStatus(checkIn.Status)
		builder.WriteString(fmt.Sprintf("*%s*  %s\n", name, status))

		if checkIn.Ticket != "" {
			builder.WriteString(fmt.Sprintf("    Ticket: `%s`\n", checkIn.Ticket))
		}

		if checkIn.Message != "" {
			builder.WriteString(fmt.Sprintf("    %s\n", checkIn.Message))
		}

		if len(checkIn.FilesTouching) > 0 {
			builder.WriteString(fmt.Sprintf("    Files: %s\n", strings.Join(checkIn.FilesTouching, ", ")))
		}

		builder.WriteString("\n")
	}

	return builder.String()
}

func displayNameForStatus(role agent.Role, personas map[agent.Role]Persona) string {
	if personas == nil {
		return string(role)
	}

	persona, ok := personas[role]
	if !ok {
		return string(role)
	}

	return persona.DisplayName()
}

func formatSlackStatus(status coordination.Status) string {
	switch status {
	case coordination.StatusWorking:
		return "`working`"
	case coordination.StatusBlocked:
		return "`blocked`"
	case coordination.StatusReviewing:
		return "`reviewing`"
	case coordination.StatusIdle:
		return "_idle_"
	case coordination.StatusPaused:
		return "`paused`"
	}
	return string(status)
}
