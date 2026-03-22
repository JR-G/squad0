package slack

import (
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/coordination"
)

// FormatStatusForSlack renders agent check-ins as Slack-formatted text.
func FormatStatusForSlack(checkIns []coordination.CheckIn) string {
	if len(checkIns) == 0 {
		return "No agents registered yet."
	}

	var builder strings.Builder
	builder.WriteString("*Agent Status*\n\n")

	for _, checkIn := range checkIns {
		status := formatSlackStatus(checkIn.Status)
		ticket := "—"
		if checkIn.Ticket != "" {
			ticket = checkIn.Ticket
		}

		fmt.Fprintf(&builder, "• *%s*  %s  %s\n", checkIn.Agent, status, ticket)
	}

	return builder.String()
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
