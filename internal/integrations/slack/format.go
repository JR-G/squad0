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
	builder.WriteString("*Agent Status*\n\n")

	for _, checkIn := range checkIns {
		name := displayNameForStatus(checkIn.Agent, personas)
		status := formatSlackStatus(checkIn.Status)
		ticket := "—"
		if checkIn.Ticket != "" {
			ticket = checkIn.Ticket
		}

		fmt.Fprintf(&builder, "• *%s*  %s  %s\n", name, status, ticket)
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
	}
	return string(status)
}
