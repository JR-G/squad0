package health_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	"github.com/stretchr/testify/assert"
)

func TestFormatAlert_SlowAndStuckStates(t *testing.T) {
	t.Parallel()

	slow := health.FormatAlertForTest(health.AgentHealth{
		Role: agent.RoleEngineer1, State: health.StateSlow,
	})
	stuck := health.FormatAlertForTest(health.AgentHealth{
		Role: agent.RoleEngineer2, State: health.StateStuck,
	})

	assert.Contains(t, slow, "taking longer than expected")
	assert.Contains(t, stuck, "no progress for an extended period")
}

func TestFormatAlert_IdleState_ReturnsMinimal(t *testing.T) {
	t.Parallel()

	result := health.FormatAlertForTest(health.AgentHealth{
		Role: agent.RolePM, State: health.StateIdle,
	})

	assert.Contains(t, result, "idle")
	assert.NotContains(t, result, "consecutive errors")
}
