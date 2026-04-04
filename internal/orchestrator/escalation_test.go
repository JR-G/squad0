package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
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

func TestEscalationTracker_CheckStale_ReturnsAndIncrementsEscalations(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{
		Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-ST1",
	}

	// Manually track with a backdated time to simulate staleness.
	tracker.Track(sit)
	tracker.BackdateForTest(sit.Key(), 5*time.Hour)

	stale := tracker.CheckStale()
	assert.Len(t, stale, 1)
	assert.Equal(t, "JAM-ST1", stale[0].Ticket)
}

func TestEscalationTracker_AutoBlocked_AfterMaxEscalations(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{
		Type:        orchestrator.SitRepeatedFailure,
		Ticket:      "JAM-BLK1",
		Escalations: 2,
	}

	tracker.Track(sit)
	tracker.BackdateForTest(sit.Key(), 5*time.Hour)

	blocked := tracker.AutoBlocked()
	assert.Len(t, blocked, 1)
	assert.Equal(t, "JAM-BLK1", blocked[0].Ticket)

	// Should be removed after auto-blocking.
	assert.Equal(t, 0, tracker.Len())
}

func TestRunEscalationCheck_StaleItem_ReEscalates(t *testing.T) {
	t.Parallel()

	orch := buildMinimalOrchestrator(t)
	queue := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(queue)

	tracker := orchestrator.NewEscalationTracker()
	orch.SetEscalationTracker(tracker)

	sit := orchestrator.Situation{
		Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-RE1",
		Severity: orchestrator.SeverityWarning,
	}
	tracker.Track(sit)
	tracker.BackdateForTest(sit.Key(), 5*time.Hour)

	orch.RunEscalationCheckForTest(t)

	// The stale item should be re-queued via Escalate.
	assert.GreaterOrEqual(t, queue.Len(), 1, "stale item should be re-escalated")
}

func TestRunEscalationCheck_AutoBlocked_PostsToTriage(t *testing.T) {
	t.Parallel()

	orch := buildMinimalOrchestrator(t)
	queue := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(queue)

	tracker := orchestrator.NewEscalationTracker()
	orch.SetEscalationTracker(tracker)

	sit := orchestrator.Situation{
		Type: orchestrator.SitRepeatedFailure, Ticket: "JAM-AB1",
		Severity: orchestrator.SeverityCritical, Escalations: 2,
	}
	tracker.Track(sit)
	tracker.BackdateForTest(sit.Key(), 5*time.Hour)

	orch.RunEscalationCheckForTest(t)

	// Auto-blocked item should be removed from tracker.
	assert.Equal(t, 0, tracker.Len(), "auto-blocked item should be removed")
}

func TestBumpSeverity_CriticalStaysCritical(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	sit := orchestrator.Situation{
		Type: orchestrator.SitRepeatedFailure, Severity: orchestrator.SeverityCritical,
		Ticket: "JAM-BS1",
	}

	queue.Escalate(sit)
	items := queue.Drain()
	assert.Len(t, items, 1)
	assert.Equal(t, orchestrator.SeverityCritical, items[0].Severity)
}

func TestSensePipelineDrift_WorkingAgent_NotFlagged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, queue := newSensorOrchestrator(t)

	itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-NF1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test/repo/pull/3"))

	// Engineer is working (not idle) — drift should NOT be flagged.
	require.NoError(t, orch.CheckIns().Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	}))

	orch.RunSensorsForTest(t)
	assert.Equal(t, 0, queue.Len(), "working engineer should not trigger drift")
}

func TestSenseRepeatedFailures_CriticalAfter4(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, queue := newSensorOrchestrator(t)

	for range 5 {
		itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
			Ticket: "JAM-C4", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
		})
		require.NoError(t, err)
		require.NoError(t, pipeStore.Advance(ctx, itemID, pipeline.StageFailed))
	}

	orch.RunSensorsForTest(t)

	items := queue.Drain()
	require.Len(t, items, 1)
	assert.Equal(t, orchestrator.SeverityCritical, items[0].Severity, "4+ failures should be critical")
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
