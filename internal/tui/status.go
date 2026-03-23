//nolint:misspell // lipgloss.Color is the library's API
package tui

import (
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	"github.com/charmbracelet/lipgloss"
)

// FormatAgentStatus renders a table of agent statuses.
func FormatAgentStatus(checkIns []coordination.CheckIn, healthStates []health.AgentHealth) string {
	var builder strings.Builder

	builder.WriteString(Section("Agent Status"))
	builder.WriteString("\n")

	healthMap := make(map[string]health.AgentHealth, len(healthStates))
	for _, state := range healthStates {
		healthMap[string(state.Role)] = state
	}

	for _, checkIn := range checkIns {
		agentHealth, hasHealth := healthMap[string(checkIn.Agent)]
		line := formatAgentLine(checkIn, agentHealth, hasHealth)
		builder.WriteString(line)
	}

	return builder.String()
}

func formatAgentLine(checkIn coordination.CheckIn, agentHealth health.AgentHealth, hasHealth bool) string {
	name := AgentName.Render(fmt.Sprintf("%-14s", checkIn.Agent))
	status := renderStatus(checkIn.Status)
	ticket := renderTicket(checkIn.Ticket)

	healthBadge := ""
	if hasHealth {
		healthBadge = renderHealthBadge(agentHealth.State)
	}

	return fmt.Sprintf("  %s %s %s %s\n", name, status, ticket, healthBadge)
}

func renderStatus(status coordination.Status) string {
	switch status {
	case coordination.StatusWorking:
		return StatusWorking.Render("working")
	case coordination.StatusBlocked:
		return Warning.Render("blocked")
	case coordination.StatusReviewing:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#8B5CF6")).Bold(true).Render("reviewing")
	case coordination.StatusIdle:
		return StatusIdle.Render("idle")
	case coordination.StatusPaused:
		return Warning.Render("paused")
	}

	return Muted.Render(string(status))
}

func renderTicket(ticket string) string {
	if ticket == "" {
		return Muted.Render("—")
	}
	return ticket
}

func renderHealthBadge(state health.State) string {
	switch state {
	case health.StateHealthy:
		return Success.Render("●")
	case health.StateSlow:
		return Warning.Render("●")
	case health.StateStuck:
		return Error.Render("●")
	case health.StateFailing:
		return StatusFailing.Render("●")
	case health.StateIdle:
		return Muted.Render("○")
	}
	return ""
}

// FormatSecretsList renders the secrets status with colours.
func FormatSecretsList(status map[string]bool) string {
	var builder strings.Builder

	builder.WriteString(Section("Secrets"))
	builder.WriteString("\n")

	for name, isSet := range status {
		icon := Cross
		stateText := Error.Render("not set")
		if isSet {
			icon = Checkmark
			stateText = Success.Render("set")
		}
		fmt.Fprintf(&builder, "  %s %-24s %s\n", icon, name, stateText)
	}

	return builder.String()
}
