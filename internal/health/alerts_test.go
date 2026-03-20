package health_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	"github.com/stretchr/testify/assert"
)

func TestFormatHealthSummary_IncludesAllAgents(t *testing.T) {
	t.Parallel()

	states := []health.AgentHealth{
		{Role: agent.RoleEngineer1, State: health.StateHealthy},
		{Role: agent.RoleEngineer2, State: health.StateFailing, ErrorCount: 3, LastError: "timeout"},
		{Role: agent.RolePM, State: health.StateIdle},
	}

	summary := health.FormatHealthSummary(states)

	assert.Contains(t, summary, "engineer-1")
	assert.Contains(t, summary, "engineer-2")
	assert.Contains(t, summary, "pm")
	assert.Contains(t, summary, "healthy")
	assert.Contains(t, summary, "failing")
	assert.Contains(t, summary, "timeout")
	assert.Contains(t, summary, "3 errors")
}

func TestFormatHealthSummary_EmptyStates(t *testing.T) {
	t.Parallel()

	summary := health.FormatHealthSummary(nil)

	assert.Contains(t, summary, "Agent Health")
}

func TestFormatHealthSummary_SlowAndStuckStates(t *testing.T) {
	t.Parallel()

	states := []health.AgentHealth{
		{Role: agent.RoleEngineer1, State: health.StateSlow},
		{Role: agent.RoleEngineer2, State: health.StateStuck},
	}

	summary := health.FormatHealthSummary(states)

	assert.Contains(t, summary, "SLOW")
	assert.Contains(t, summary, "STUCK")
}
