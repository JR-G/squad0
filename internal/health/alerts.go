package health

import (
	"context"
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
)

// Alerter posts health alerts to Slack when agents are unhealthy.
type Alerter struct {
	monitor *Monitor
	bot     *slack.Bot
	channel string
}

// NewAlerter creates an Alerter that posts to the given Slack channel.
func NewAlerter(monitor *Monitor, bot *slack.Bot, channel string) *Alerter {
	return &Alerter{
		monitor: monitor,
		bot:     bot,
		channel: channel,
	}
}

// CheckAndAlert evaluates agent health and posts alerts for any
// unhealthy agents. Returns the number of alerts sent.
func (alerter *Alerter) CheckAndAlert(ctx context.Context) (int, error) {
	alerter.monitor.Evaluate()

	unhealthy := alerter.monitor.UnhealthyAgents()
	if len(unhealthy) == 0 {
		return 0, nil
	}

	alertCount := 0

	for _, health := range unhealthy {
		msg := formatAlert(health)
		err := alerter.bot.PostAsRole(ctx, alerter.channel, msg, agent.RolePM)
		if err != nil {
			return alertCount, fmt.Errorf("posting alert for %s: %w", health.Role, err)
		}
		alertCount++
	}

	return alertCount, nil
}

// FormatHealthSummary returns a human-readable summary of all agent
// health states.
func FormatHealthSummary(healthStates []AgentHealth) string {
	var builder strings.Builder

	builder.WriteString("*Agent Health*\n\n")

	for _, health := range healthStates {
		icon := stateIcon(health.State)
		fmt.Fprintf(&builder, "%s *%s* — %s", icon, health.Role, health.State)

		if health.LastError != "" {
			fmt.Fprintf(&builder, " (last error: %s)", health.LastError)
		}

		if health.ErrorCount > 0 {
			fmt.Fprintf(&builder, " [%d errors]", health.ErrorCount)
		}

		builder.WriteString("\n")
	}

	return builder.String()
}

func formatAlert(health AgentHealth) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "*%s* is %s", health.Role, health.State)

	switch health.State {
	case StateSlow:
		builder.WriteString(" — session is taking longer than expected")
	case StateStuck:
		builder.WriteString(" — no progress for an extended period")
	case StateFailing:
		fmt.Fprintf(&builder, " — %d consecutive errors", health.ErrorCount)
		appendLastError(&builder, health.LastError)
	case StateHealthy, StateIdle:
		return builder.String()
	}

	return builder.String()
}

func appendLastError(builder *strings.Builder, lastError string) {
	if lastError != "" {
		fmt.Fprintf(builder, ": %s", lastError)
	}
}

func stateIcon(state State) string {
	switch state {
	case StateHealthy:
		return "OK"
	case StateSlow:
		return "SLOW"
	case StateStuck:
		return "STUCK"
	case StateFailing:
		return "FAIL"
	case StateIdle:
		return "IDLE"
	}
	return "?"
}
