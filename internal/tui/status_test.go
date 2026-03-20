package tui_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	"github.com/JR-G/squad0/internal/tui"
	"github.com/stretchr/testify/assert"
)

func TestFormatAgentStatus_ShowsAllAgents(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, Ticket: "SQ-42"},
		{Agent: agent.RoleEngineer2, Status: coordination.StatusIdle},
	}

	result := tui.FormatAgentStatus(checkIns, nil)

	assert.Contains(t, result, "engineer-1")
	assert.Contains(t, result, "engineer-2")
	assert.Contains(t, result, "SQ-42")
}

func TestFormatAgentStatus_WithHealthStates(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking},
	}
	healthStates := []health.AgentHealth{
		{Role: agent.RoleEngineer1, State: health.StateHealthy},
	}

	result := tui.FormatAgentStatus(checkIns, healthStates)

	assert.Contains(t, result, "engineer-1")
	assert.Contains(t, result, "●")
}

func TestFormatAgentStatus_AllStatuses(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking},
		{Agent: agent.RoleEngineer2, Status: coordination.StatusBlocked},
		{Agent: agent.RoleEngineer3, Status: coordination.StatusIdle},
		{Agent: agent.RoleReviewer, Status: coordination.StatusReviewing},
	}

	result := tui.FormatAgentStatus(checkIns, nil)

	assert.Contains(t, result, "working")
	assert.Contains(t, result, "blocked")
	assert.Contains(t, result, "idle")
	assert.Contains(t, result, "reviewing")
}

func TestFormatAgentStatus_EmptyTicket(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{Agent: agent.RolePM, Status: coordination.StatusIdle, Ticket: ""},
	}

	result := tui.FormatAgentStatus(checkIns, nil)

	assert.Contains(t, result, "—")
}

func TestFormatAgentStatus_AllHealthBadges(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking},
		{Agent: agent.RoleEngineer2, Status: coordination.StatusWorking},
		{Agent: agent.RoleEngineer3, Status: coordination.StatusWorking},
		{Agent: agent.RolePM, Status: coordination.StatusIdle},
		{Agent: agent.RoleReviewer, Status: coordination.StatusWorking},
	}
	healthStates := []health.AgentHealth{
		{Role: agent.RoleEngineer1, State: health.StateHealthy},
		{Role: agent.RoleEngineer2, State: health.StateSlow},
		{Role: agent.RoleEngineer3, State: health.StateStuck},
		{Role: agent.RolePM, State: health.StateIdle},
		{Role: agent.RoleReviewer, State: health.StateFailing},
	}

	result := tui.FormatAgentStatus(checkIns, healthStates)

	assert.NotEmpty(t, result)
}

func TestFormatSecretsList_ShowsSetAndNotSet(t *testing.T) {
	t.Parallel()

	status := map[string]bool{
		"SLACK_BOT_TOKEN": true,
		"SLACK_APP_TOKEN": false,
	}

	result := tui.FormatSecretsList(status)

	assert.Contains(t, result, "SLACK_BOT_TOKEN")
	assert.Contains(t, result, "SLACK_APP_TOKEN")
	assert.Contains(t, result, "✓")
	assert.Contains(t, result, "✗")
}
