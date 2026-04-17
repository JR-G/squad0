package health_test

import (
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	"github.com/stretchr/testify/assert"
)

func TestClassifySessionDuration_WellUnderHalf_ReturnsHealthy(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 10 * time.Second,
	})

	mon.RecordSessionStart(agent.RoleEngineer1)
	// Evaluate immediately — near-zero elapsed, well under half of 10s
	mon.Evaluate()

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateHealthy, state.State)
}

func TestClassifySessionDuration_ClearlyOverHalf_ReturnsSlow(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		// Headroom for goroutine scheduler under concurrent test load:
		// 60ms sleep against 100ms max is clearly Slow (>50%) without
		// risking Stuck if the scheduler delays past 100ms.
		MaxSessionTime: 100 * time.Millisecond,
	})

	mon.RecordSessionStart(agent.RoleEngineer1)
	time.Sleep(60 * time.Millisecond)
	mon.Evaluate()

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateSlow, state.State)
}

func TestClassifySessionDuration_ClearlyOverMax_ReturnsStuck(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 50 * time.Millisecond,
	})

	mon.RecordSessionStart(agent.RoleEngineer1)
	time.Sleep(150 * time.Millisecond)
	mon.Evaluate()

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateStuck, state.State)
}

func TestEvaluateAgent_NilSessionStart_SetsIdle(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 3,
	})

	// Agent starts idle, never had a session
	mon.Evaluate()

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateIdle, state.State)
}

func TestEvaluateAgent_NilSessionStartAfterEnd_SetsIdle(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 3,
	})

	// Start then end a session — SessionStart becomes nil
	mon.RecordSessionStart(agent.RoleEngineer1)
	mon.RecordSessionEnd(agent.RoleEngineer1, "SQ-1", true)

	mon.Evaluate()

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateIdle, state.State)
	assert.Nil(t, state.SessionStart)
}

func TestEvaluateAgent_FailingWithNilSession_StaysFailing(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 2,
	})

	// Push agent to failing via errors — no active session
	mon.RecordError(agent.RoleEngineer1, "err1")
	mon.RecordError(agent.RoleEngineer1, "err2")

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateFailing, state.State)

	// Evaluate should NOT change failing state to idle even though
	// SessionStart is nil (the early return for StateFailing fires first)
	mon.Evaluate()

	state, _ = mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateFailing, state.State)
}

func TestRecordSessionStart_OverridesFailingState(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 2,
	})

	// Push to failing
	mon.RecordError(agent.RoleEngineer1, "err1")
	mon.RecordError(agent.RoleEngineer1, "err2")

	// Starting a new session resets to healthy
	mon.RecordSessionStart(agent.RoleEngineer1)

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateHealthy, state.State)
	assert.NotNil(t, state.SessionStart)
}

func TestClassifySessionDuration_ZeroElapsed_ReturnsHealthy(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	mon.RecordSessionStart(agent.RoleEngineer1)
	// Evaluate immediately — near-zero elapsed
	mon.Evaluate()

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateHealthy, state.State)
}

func TestMonitor_RecordSessionEnd_FailureBelowMax_StaysIdle(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxConsecutiveErrors: 5,
	})

	mon.RecordSessionEnd(agent.RoleEngineer1, "SQ-1", false)

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.Equal(t, health.StateIdle, state.State)
	assert.Equal(t, 1, state.ErrorCount)
}

func TestMonitor_RecordError_BelowMax_DoesNotSetFailing(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxConsecutiveErrors: 10,
	})

	mon.RecordError(agent.RoleEngineer1, "single error")

	state, _ := mon.GetHealth(agent.RoleEngineer1)
	assert.NotEqual(t, health.StateFailing, state.State)
	assert.Equal(t, "single error", state.LastError)
}
