package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscalationTracker_Track_AddsSituation(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	tracker.Track(orchestrator.Situation{
		Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-1",
	})

	assert.Equal(t, 1, tracker.Len())
}

func TestEscalationTracker_Track_DeduplicatesSameKey(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{
		Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-1",
	}

	tracker.Track(sit)
	tracker.Track(sit) // Same key.

	assert.Equal(t, 1, tracker.Len())
}

func TestEscalationTracker_Acknowledge_PreventsStale(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{
		Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-1",
	}

	tracker.Track(sit)
	tracker.Acknowledge(sit.Key())

	// CheckStale should return nothing — item is acknowledged.
	stale := tracker.CheckStale()
	assert.Empty(t, stale)
}

func TestEscalationTracker_CheckStale_FreshItems_NotStale(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	tracker.Track(orchestrator.Situation{
		Type: orchestrator.SitRepeatedFailure, Ticket: "JAM-2",
	})

	// Just tracked — shouldn't be stale yet.
	stale := tracker.CheckStale()
	assert.Empty(t, stale)
}

func TestEscalationTracker_Remove_ClearsItem(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{
		Type: orchestrator.SitPipelineDrift, Ticket: "JAM-3",
	}

	tracker.Track(sit)
	assert.Equal(t, 1, tracker.Len())

	tracker.Remove(sit.Key())
	assert.Equal(t, 0, tracker.Len())
}

func TestEscalationTracker_AutoBlocked_FreshItems_NotBlocked(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	tracker.Track(orchestrator.Situation{
		Type:        orchestrator.SitRepeatedFailure,
		Ticket:      "JAM-4",
		Escalations: 3,
	})

	// Even with high escalation count, fresh items aren't auto-blocked.
	blocked := tracker.AutoBlocked()
	assert.Empty(t, blocked)
}

func TestSensorRunSensors_NilQueue_DoesNotPanic(t *testing.T) {
	t.Parallel()

	orch := buildMinimalOrchestrator(t)
	// No situation queue set — should not panic.
	orch.RunSensorsForTest(t)
}

func TestSensorRunSensors_NoPipeline_DoesNotPanic(t *testing.T) {
	t.Parallel()

	orch := buildMinimalOrchestrator(t)
	orch.SetSituationQueue(orchestrator.NewSituationQueue())
	// No pipeline store — should not panic.
	orch.RunSensorsForTest(t)
}

func TestEscalationRunCheck_NilTracker_DoesNotPanic(t *testing.T) {
	t.Parallel()

	orch := buildMinimalOrchestrator(t)
	orch.SetSituationQueue(orchestrator.NewSituationQueue())
	// No escalation tracker — should not panic.
	orch.RunEscalationCheckForTest(t)
}

func buildMinimalOrchestrator(t *testing.T) *orchestrator.Orchestrator {
	t.Helper()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	return orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
}
