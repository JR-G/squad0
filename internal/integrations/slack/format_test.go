package slack_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/stretchr/testify/assert"
)

func TestFormatStatusForSlack_ShowsAllAgents(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, Ticket: "SQ-42"},
		{Agent: agent.RolePM, Status: coordination.StatusIdle},
	}

	result := slack.FormatStatusForSlack(checkIns, nil)

	assert.Contains(t, result, "engineer-1")
	assert.Contains(t, result, "`working`")
	assert.Contains(t, result, "SQ-42")
	assert.Contains(t, result, "pm")
	assert.Contains(t, result, "_idle_")
}

func TestFormatStatusForSlack_EmptyCheckIns(t *testing.T) {
	t.Parallel()

	result := slack.FormatStatusForSlack(nil, nil)

	assert.Contains(t, result, "No agents")
}

func TestFormatStatusForSlack_AllStatuses(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking},
		{Agent: agent.RoleEngineer2, Status: coordination.StatusBlocked},
		{Agent: agent.RoleEngineer3, Status: coordination.StatusIdle},
		{Agent: agent.RoleReviewer, Status: coordination.StatusReviewing},
		{Agent: agent.RoleDesigner, Status: coordination.StatusPaused},
	}

	result := slack.FormatStatusForSlack(checkIns, nil)

	assert.Contains(t, result, "`working`")
	assert.Contains(t, result, "`blocked`")
	assert.Contains(t, result, "_idle_")
	assert.Contains(t, result, "`reviewing`")
	assert.Contains(t, result, "`paused`")
}

func TestFormatStatusForSlack_UnknownStatus(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{Agent: agent.RolePM, Status: coordination.Status("unknown")},
	}

	result := slack.FormatStatusForSlack(checkIns, nil)

	assert.Contains(t, result, "unknown")
}

func TestFormatStatusForSlack_EmptyTicket_OmitsTicketLine(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{Agent: agent.RolePM, Status: coordination.StatusIdle, Ticket: ""},
	}

	result := slack.FormatStatusForSlack(checkIns, nil)

	assert.NotContains(t, result, "Ticket:")
}

func TestFormatStatusForSlack_WithMessageAndFiles(t *testing.T) {
	t.Parallel()

	checkIns := []coordination.CheckIn{
		{
			Agent:         agent.RoleEngineer1,
			Status:        coordination.StatusWorking,
			Ticket:        "JAM-17",
			Message:       "working on JAM-17",
			FilesTouching: []string{"auth/middleware.go", "auth/oauth.go"},
		},
	}

	result := slack.FormatStatusForSlack(checkIns, nil)

	assert.Contains(t, result, "JAM-17")
	assert.Contains(t, result, "working on JAM-17")
	assert.Contains(t, result, "auth/middleware.go")
}

func TestDisplayName_WithChosenName_ShowsNameAndRole(t *testing.T) {
	t.Parallel()

	persona := slack.Persona{Role: agent.RolePM, Name: "Nova"}

	assert.Equal(t, "Nova — PM", persona.DisplayName())
}

func TestDisplayName_NoChosenName_ShowsRoleTitle(t *testing.T) {
	t.Parallel()

	persona := slack.Persona{Role: agent.RolePM, Name: "pm"}

	assert.Equal(t, "PM", persona.DisplayName())
}

func TestDisplayName_Engineer_ShowsEngineer(t *testing.T) {
	t.Parallel()

	persona := slack.Persona{Role: agent.RoleEngineer1, Name: "Ada"}

	assert.Equal(t, "Ada — Engineer", persona.DisplayName())
}

func TestDisplayName_AllRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role     agent.Role
		name     string
		expected string
	}{
		{agent.RolePM, "pm", "PM"},
		{agent.RoleTechLead, "Rex", "Rex — Tech Lead"},
		{agent.RoleEngineer1, "Ada", "Ada — Engineer"},
		{agent.RoleEngineer2, "engineer-2", "Engineer"},
		{agent.RoleReviewer, "reviewer", "Reviewer"},
		{agent.RoleDesigner, "Iris", "Iris — Designer"},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			t.Parallel()
			persona := slack.Persona{Role: tt.role, Name: tt.name}
			assert.Equal(t, tt.expected, persona.DisplayName())
		})
	}
}
