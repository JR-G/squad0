package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// situationqueue.go — Push/Drain/Resolve/Dedup coverage
// ---------------------------------------------------------------------------

func TestSituationQueue_Dedup_SameKey_SkipsSecond(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-D1", Engineer: agent.RoleEngineer1}
	queue.Push(sit)
	queue.Push(sit)

	drained := queue.Drain()
	assert.Len(t, drained, 1, "duplicate should be deduped")
}

func TestSituationQueue_Resolve_AllowsRepush(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-R1", Engineer: agent.RoleEngineer1}
	queue.Push(sit)
	drained := queue.Drain()
	require.Len(t, drained, 1)

	queue.Resolve(sit.Key())

	// After resolving, a re-push within cooldown should still be deduped.
	queue.Push(sit)
	drained2 := queue.Drain()
	assert.Empty(t, drained2, "resolved within cooldown should be deduped")
}

func TestSituationQueue_DrainEmpty_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	queue := orchestrator.NewSituationQueue()
	assert.Empty(t, queue.Drain())
}

// ---------------------------------------------------------------------------
// escalation.go — Track/Acknowledge coverage
// ---------------------------------------------------------------------------

func TestEscalationTracker_Track_DoesNotPanic(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-E1"}

	assert.NotPanics(t, func() {
		tracker.Track(sit)
		tracker.Track(sit)
	})
}

func TestEscalationTracker_Acknowledge_DoesNotPanic(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-A1"}

	tracker.Track(sit)
	tracker.Acknowledge(sit.Key())

	// Acknowledging again should not panic.
	tracker.Acknowledge(sit.Key())
}

// ---------------------------------------------------------------------------
// sensors.go — various sensor paths
// ---------------------------------------------------------------------------

func TestSensors_StaleWorking_DetectsOldItems(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, memDB),
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{}, agents, checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	situations := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(situations)

	// Create a working item that's old enough to trigger the sensor.
	_, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-STALE", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, createErr)

	// Backdate the item to trigger stale detection.
	_, execErr := sqlDB.ExecContext(ctx, `UPDATE work_items SET updated_at = datetime('now', '-2 hours')`)
	require.NoError(t, execErr)

	orch.RunSensorsForTest(t)

	drained := situations.Drain()
	staleFound := false
	for _, sit := range drained {
		if sit.Type == orchestrator.SitStaleWorkingAgent && sit.Ticket == "JAM-STALE" {
			staleFound = true
		}
	}
	assert.True(t, staleFound, "sensor should detect stale working item")
}

// ---------------------------------------------------------------------------
// smartassign.go — DeferTicket expiry
// ---------------------------------------------------------------------------

func TestSmartAssigner_DeferTicket_ExpiresOverTime(t *testing.T) {
	t.Parallel()

	sa := orchestrator.NewSmartAssigner(nil)
	sa.DeferTicket("JAM-EXP", 1*time.Millisecond)

	time.Sleep(5 * time.Millisecond)
	assert.False(t, sa.IsDeferred("JAM-EXP"), "should expire after duration")
}

// ---------------------------------------------------------------------------
// deferral.go — additional signal patterns
// ---------------------------------------------------------------------------

func TestDeferralSignal_StopPattern(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsDeferralSignalForTest("Stop. JAM-20 is not ready."))
}

func TestDeferralSignal_PausedPattern(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsDeferralSignalForTest("JAM-20 is paused until further notice"))
}

func TestDeferralSignal_WaitPattern(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsDeferralSignalForTest("Wait on JAM-20 until the spec is done"))
}
