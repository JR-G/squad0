package orchestrator_test

import (
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthSupervisor_NilMonitor_PassesAllEngineers(t *testing.T) {
	t.Parallel()

	sup := orchestrator.NewHealthSupervisor(nil)

	got := sup.FilterHealthyEngineers([]agent.Role{
		agent.RolePM, agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleDesigner,
	})

	// Non-engineer roles are filtered first; nil monitor doesn't affect that.
	assert.Contains(t, got, agent.RoleEngineer1)
	assert.Contains(t, got, agent.RoleEngineer2)
	assert.NotContains(t, got, agent.RolePM)
	assert.NotContains(t, got, agent.RoleDesigner)
}

func TestHealthSupervisor_NilMonitor_RecordCallsAreNoop(t *testing.T) {
	t.Parallel()

	sup := orchestrator.NewHealthSupervisor(nil)

	assert.NotPanics(t, func() {
		sup.RecordSessionStart(agent.RoleEngineer1)
		sup.RecordSessionEnd(agent.RoleEngineer1, "JAM-1", true)
	})
}

func TestHealthSupervisor_FailingAgent_Excluded(t *testing.T) {
	t.Parallel()

	monitor := health.NewMonitor(
		[]agent.Role{agent.RoleEngineer1, agent.RoleEngineer2},
		health.MonitorConfig{
			MaxIdleTime:          time.Hour,
			MaxSessionTime:       time.Hour,
			MaxConsecutiveErrors: 2,
		},
	)

	// Push engineer-2 into failing state by recording 2 consecutive errors.
	monitor.RecordSessionStart(agent.RoleEngineer2)
	monitor.RecordSessionEnd(agent.RoleEngineer2, "JAM-1", false)
	monitor.RecordSessionStart(agent.RoleEngineer2)
	monitor.RecordSessionEnd(agent.RoleEngineer2, "JAM-2", false)

	sup := orchestrator.NewHealthSupervisor(monitor)

	got := sup.FilterHealthyEngineers([]agent.Role{agent.RoleEngineer1, agent.RoleEngineer2})

	require.Len(t, got, 1)
	assert.Equal(t, agent.RoleEngineer1, got[0])
}

func TestHealthSupervisor_HealthyAgent_RecordedAndPassed(t *testing.T) {
	t.Parallel()

	monitor := health.NewMonitor(
		[]agent.Role{agent.RoleEngineer1},
		health.MonitorConfig{
			MaxIdleTime:          time.Hour,
			MaxSessionTime:       time.Hour,
			MaxConsecutiveErrors: 3,
		},
	)
	sup := orchestrator.NewHealthSupervisor(monitor)

	sup.RecordSessionStart(agent.RoleEngineer1)
	sup.RecordSessionEnd(agent.RoleEngineer1, "JAM-1", true)

	got := sup.FilterHealthyEngineers([]agent.Role{agent.RoleEngineer1})

	assert.Equal(t, []agent.Role{agent.RoleEngineer1}, got)
}

func TestHealthSupervisor_Monitor_ReturnsUnderlying(t *testing.T) {
	t.Parallel()

	monitor := health.NewMonitor(nil, health.MonitorConfig{})
	sup := orchestrator.NewHealthSupervisor(monitor)

	assert.Same(t, monitor, sup.Monitor())
	assert.Nil(t, orchestrator.NewHealthSupervisor(nil).Monitor())
}
