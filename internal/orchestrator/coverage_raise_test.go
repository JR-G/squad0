package orchestrator_test

import (
	"context"
	"database/sql"
	"fmt"
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

func TestSensors_UnmergedApproved_DetectsStaleApproval(t *testing.T) {
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

	orch := orchestrator.NewOrchestrator(orchestrator.Config{}, agents, checkIns, nil, nil)
	orch.SetPipeline(pipeStore)
	situations := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(situations)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-APPROVED", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved,
	})
	_ = pipeStore.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/1")
	// Backdate to trigger stale approved detection.
	_, _ = sqlDB.ExecContext(ctx, `UPDATE work_items SET updated_at = datetime('now', '-1 hour')`)

	orch.RunSensorsForTest(t)

	approvedFound := false
	for _, sit := range situations.Drain() {
		if sit.Type == orchestrator.SitUnmergedApprovedPR && sit.Ticket == "JAM-APPROVED" {
			approvedFound = true
		}
	}
	assert.True(t, approvedFound)
}

func TestSensors_PipelineDrift_DetectsIdleWithOpenPR(t *testing.T) {
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

	orch := orchestrator.NewOrchestrator(orchestrator.Config{}, agents, checkIns, nil, nil)
	orch.SetPipeline(pipeStore)
	situations := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(situations)

	// Engineer is idle but has an open PR.
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusIdle, FilesTouching: []string{},
	}))
	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-DRIFT", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	_ = pipeStore.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/2")

	orch.RunSensorsForTest(t)

	driftFound := false
	for _, sit := range situations.Drain() {
		if sit.Type == orchestrator.SitPipelineDrift && sit.Ticket == "JAM-DRIFT" {
			driftFound = true
		}
	}
	assert.True(t, driftFound)
}

// ---------------------------------------------------------------------------
// reconcileWithFetcher + FilterHealthyEngineers — zero coverage exports
// ---------------------------------------------------------------------------

func TestReconcileWithFetcher_FakeFetcher_Reconciles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-RF", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	_ = pipeStore.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/77")

	orch := newReconcileOrch(t, pipeStore)

	orch.ReconcileWithFetcherForTest(ctx, func(_ context.Context) (map[string]orchestrator.PRState, error) {
		return map[string]orchestrator.PRState{
			"https://github.com/org/repo/pull/77": {State: "MERGED"},
		}, nil
	})

	item, _ := pipeStore.GetByID(ctx, itemID)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}

func TestNewGHPRStateFetcher_ReturnsFunction(t *testing.T) {
	t.Parallel()
	fetcher := orchestrator.NewGHPRStateFetcher("/tmp")
	assert.NotNil(t, fetcher)
	// Call it — will fail (no gh in /tmp) but should not panic.
	_, err := fetcher(context.Background())
	assert.Error(t, err)
}

func TestReconcileWithFetcher_Error_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	orch := newReconcileOrch(t, pipeStore)

	assert.NotPanics(t, func() {
		orch.ReconcileWithFetcherForTest(ctx, func(_ context.Context) (map[string]orchestrator.PRState, error) {
			return nil, fmt.Errorf("gh not available")
		})
	})
}

func TestFilterHealthyEngineers_NilMonitor_ReturnsAllEngineers(t *testing.T) {
	t.Parallel()

	orch := orchestrator.NewOrchestrator(orchestrator.Config{}, nil, nil, nil, nil)
	roles := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2}
	filtered := orch.FilterHealthyEngineersForTest(roles)
	assert.Len(t, filtered, 2)
}

func TestEscalationTracker_Acknowledge_UntrackedKey_DoesNotPanic(t *testing.T) {
	t.Parallel()
	tracker := orchestrator.NewEscalationTracker()
	tracker.Acknowledge("never-tracked-key")
}

func TestSensors_RepeatedFailures_DetectsPattern(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(orchestrator.Config{}, nil, checkIns, nil, nil)
	orch.SetPipeline(pipeStore)
	situations := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(situations)

	for range 4 {
		itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
			Ticket: "JAM-FAIL", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
		})
		_ = pipeStore.Advance(ctx, itemID, pipeline.StageFailed)
	}

	orch.RunSensorsForTest(t)

	failFound := false
	for _, sit := range situations.Drain() {
		if sit.Type == orchestrator.SitRepeatedFailure && sit.Ticket == "JAM-FAIL" {
			failFound = true
		}
	}
	assert.True(t, failFound)
}

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
