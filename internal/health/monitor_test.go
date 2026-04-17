package health_test

import (
	"context"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMonitor() *health.Monitor {
	roles := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RolePM}
	return health.NewMonitor(roles, health.MonitorConfig{
		MaxIdleTime:          10 * time.Minute,
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 3,
	})
}

func TestMonitor_NewMonitor_AllStartIdle(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()
	healthStates := mon.AllHealth(context.Background())

	assert.Len(t, healthStates, 3)
	for _, state := range healthStates {
		assert.Equal(t, health.StateIdle, state.State)
	}
}

func TestMonitor_RecordSessionStart_SetsHealthy(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()
	mon.RecordSessionStart(agent.RoleEngineer1)

	state, err := mon.GetHealth(agent.RoleEngineer1)
	require.NoError(t, err)
	assert.Equal(t, health.StateHealthy, state.State)
	assert.NotNil(t, state.SessionStart)
}

func TestMonitor_RecordSessionEnd_Success_ResetsErrors(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()
	mon.RecordError(agent.RoleEngineer1, "error 1")
	mon.RecordError(agent.RoleEngineer1, "error 2")

	mon.RecordSessionEnd(agent.RoleEngineer1, "SQ-42", true)

	state, err := mon.GetHealth(agent.RoleEngineer1)
	require.NoError(t, err)
	assert.Equal(t, 0, state.ErrorCount)
	assert.Equal(t, health.StateIdle, state.State)
}

func TestMonitor_RecordSessionEnd_Failure_IncrementsErrors(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()

	mon.RecordSessionEnd(agent.RoleEngineer1, "SQ-42", false)
	mon.RecordSessionEnd(agent.RoleEngineer1, "SQ-42", false)

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, 2, state.ErrorCount)
	assert.Equal(t, 2, state.TicketFailures["SQ-42"])
}

func TestMonitor_RecordSessionEnd_MaxErrors_SetsFailing(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()

	for i := 0; i < 3; i++ {
		mon.RecordSessionEnd(agent.RoleEngineer1, "SQ-42", false)
	}

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateFailing, state.State)
}

func TestMonitor_RecordError_SetsFailing(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()

	for i := 0; i < 3; i++ {
		mon.RecordError(agent.RoleEngineer1, "something broke")
	}

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateFailing, state.State)
	assert.Equal(t, "something broke", state.LastError)
}

func TestMonitor_Evaluate_LongSession_SetsSlow(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 100 * time.Millisecond,
	})

	mon.RecordSessionStart(agent.RoleEngineer1)
	time.Sleep(60 * time.Millisecond)
	mon.Evaluate()

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateSlow, state.State)
}

func TestMonitor_Evaluate_VeryLongSession_SetsStuck(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 100 * time.Millisecond,
	})

	mon.RecordSessionStart(agent.RoleEngineer1)
	time.Sleep(150 * time.Millisecond)
	mon.Evaluate()

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateStuck, state.State)
}

func TestMonitor_Evaluate_FailingAgent_StaysFailing(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()
	for i := 0; i < 3; i++ {
		mon.RecordError(agent.RoleEngineer1, "error")
	}

	mon.Evaluate()

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateFailing, state.State)
}

func TestMonitor_GetHealth_UnknownRole_ReturnsError(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()

	_, err := mon.GetHealth(agent.Role("unknown"))

	require.Error(t, err)
}

func TestMonitor_UnhealthyAgents_ReturnsOnlyUnhealthy(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()
	for i := 0; i < 3; i++ {
		mon.RecordError(agent.RoleEngineer1, "error")
	}

	unhealthy := mon.UnhealthyAgents()

	assert.Len(t, unhealthy, 1)
	assert.Equal(t, agent.RoleEngineer1, unhealthy[0].Role)
}

func TestMonitor_UnhealthyAgents_AllHealthy_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()

	unhealthy := mon.UnhealthyAgents()

	assert.Empty(t, unhealthy)
}

func TestMonitor_RecordSessionStart_UnknownRole_NoOp(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()

	assert.NotPanics(t, func() {
		mon.RecordSessionStart(agent.Role("unknown"))
	})
}

func TestMonitor_RecordSessionEnd_UnknownRole_NoOp(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()

	assert.NotPanics(t, func() {
		mon.RecordSessionEnd(agent.Role("unknown"), "SQ-1", true)
	})
}

func TestMonitor_RecordError_UnknownRole_NoOp(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()

	assert.NotPanics(t, func() {
		mon.RecordError(agent.Role("unknown"), "error")
	})
}
