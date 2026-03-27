package health_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	"github.com/stretchr/testify/assert"
)

func TestAlerter_SetRoster_UsesChosenNames(t *testing.T) {
	t.Parallel()

	monitor := health.NewMonitor(agent.AllRoles(), health.MonitorConfig{})
	alerter := health.NewAlerter(monitor, nil, "triage")

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Spark",
		agent.RoleEngineer2: "Nova",
	}
	alerter.SetRoster(roster)

	msg := health.FormatAlertWithNameForTest(alerter, health.AgentHealth{
		Role:  agent.RoleEngineer1,
		State: health.StateSlow,
	})

	assert.Contains(t, msg, "Spark")
	assert.NotContains(t, msg, "engineer-1")
}

func TestAlerter_NoRoster_FallsBackToRoleID(t *testing.T) {
	t.Parallel()

	monitor := health.NewMonitor(agent.AllRoles(), health.MonitorConfig{})
	alerter := health.NewAlerter(monitor, nil, "triage")

	msg := health.FormatAlertWithNameForTest(alerter, health.AgentHealth{
		Role:  agent.RoleEngineer1,
		State: health.StateStuck,
	})

	assert.Contains(t, msg, "engineer-1")
	assert.Contains(t, msg, "stuck")
}

func TestAlerter_EmptyRosterEntry_FallsBackToRoleID(t *testing.T) {
	t.Parallel()

	monitor := health.NewMonitor(agent.AllRoles(), health.MonitorConfig{})
	alerter := health.NewAlerter(monitor, nil, "triage")

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "",
	}
	alerter.SetRoster(roster)

	msg := health.FormatAlertWithNameForTest(alerter, health.AgentHealth{
		Role:       agent.RoleEngineer1,
		State:      health.StateFailing,
		ErrorCount: 5,
		LastError:  "session crash",
	})

	assert.Contains(t, msg, "engineer-1")
	assert.Contains(t, msg, "5 consecutive errors")
}

func TestAlerter_RosterWithNames_AllStates(t *testing.T) {
	t.Parallel()

	monitor := health.NewMonitor(agent.AllRoles(), health.MonitorConfig{})
	alerter := health.NewAlerter(monitor, nil, "triage")

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Spark",
		agent.RoleEngineer2: "Nova",
		agent.RolePM:        "Ada",
	}
	alerter.SetRoster(roster)

	tests := []struct {
		name     string
		health   health.AgentHealth
		contains string
	}{
		{
			name: "slow uses chosen name",
			health: health.AgentHealth{
				Role: agent.RoleEngineer1, State: health.StateSlow,
			},
			contains: "Spark",
		},
		{
			name: "stuck uses chosen name",
			health: health.AgentHealth{
				Role: agent.RoleEngineer2, State: health.StateStuck,
			},
			contains: "Nova",
		},
		{
			name: "failing uses chosen name",
			health: health.AgentHealth{
				Role: agent.RolePM, State: health.StateFailing,
				ErrorCount: 2, LastError: "rate limit",
			},
			contains: "Ada",
		},
		{
			name: "healthy uses chosen name",
			health: health.AgentHealth{
				Role: agent.RoleEngineer1, State: health.StateHealthy,
			},
			contains: "Spark",
		},
		{
			name: "idle uses chosen name",
			health: health.AgentHealth{
				Role: agent.RolePM, State: health.StateIdle,
			},
			contains: "Ada",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := health.FormatAlertWithNameForTest(alerter, tt.health)
			assert.Contains(t, msg, tt.contains)
		})
	}
}

func TestAlerter_MissingRoleInRoster_FallsBack(t *testing.T) {
	t.Parallel()

	monitor := health.NewMonitor(agent.AllRoles(), health.MonitorConfig{})
	alerter := health.NewAlerter(monitor, nil, "triage")

	// Roster has some roles but not engineer-3.
	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Spark",
	}
	alerter.SetRoster(roster)

	msg := health.FormatAlertWithNameForTest(alerter, health.AgentHealth{
		Role:  agent.RoleEngineer3,
		State: health.StateSlow,
	})

	assert.Contains(t, msg, "engineer-3")
}
