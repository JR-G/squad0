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

// --- situationqueue ---

func TestSituationQueue_Dedup(t *testing.T) {
	t.Parallel()
	queue := orchestrator.NewSituationQueue()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-D1", Engineer: agent.RoleEngineer1}
	queue.Push(sit)
	queue.Push(sit)
	assert.Len(t, queue.Drain(), 1)
}

func TestSituationQueue_ResolveAndRepush(t *testing.T) {
	t.Parallel()
	queue := orchestrator.NewSituationQueue()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-R1", Engineer: agent.RoleEngineer1}
	queue.Push(sit)
	require.Len(t, queue.Drain(), 1)
	queue.Resolve(sit.Key())
	queue.Push(sit)
	assert.Empty(t, queue.Drain())
}

func TestSituationQueue_DrainEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.NewSituationQueue().Drain())
}

// --- escalation ---

func TestEscalationTracker_TrackTwice(t *testing.T) {
	t.Parallel()
	tr := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-E1"}
	assert.NotPanics(t, func() { tr.Track(sit); tr.Track(sit) })
}

func TestEscalationTracker_AcknowledgeTwice(t *testing.T) {
	t.Parallel()
	tr := orchestrator.NewEscalationTracker()
	sit := orchestrator.Situation{Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-A1"}
	tr.Track(sit)
	tr.Acknowledge(sit.Key())
	tr.Acknowledge(sit.Key())
}

func TestEscalationTracker_AcknowledgeUntracked(t *testing.T) {
	t.Parallel()
	orchestrator.NewEscalationTracker().Acknowledge("nonexistent")
}

// --- sensors ---

func TestSensors_StaleWorking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))
	memDB, _ := memory.Open(ctx, ":memory:")
	t.Cleanup(func() { _ = memDB.Close() })
	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, memDB)}
	orch := orchestrator.NewOrchestrator(orchestrator.Config{}, agents, checkIns, nil, nil)
	orch.SetPipeline(pipeStore)
	sits := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(sits)
	_, _ = pipeStore.Create(ctx, pipeline.WorkItem{Ticket: "JAM-STALE", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking})
	_, _ = sqlDB.ExecContext(ctx, `UPDATE work_items SET updated_at = datetime('now', '-2 hours')`)
	orch.RunSensorsForTest(t)
	found := false
	for _, s := range sits.Drain() {
		if s.Type == orchestrator.SitStaleWorkingAgent {
			found = true
		}
	}
	assert.True(t, found)
}

func TestSensors_RepeatedFailures(t *testing.T) {
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
	sits := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(sits)
	for range 4 {
		id, _ := pipeStore.Create(ctx, pipeline.WorkItem{Ticket: "JAM-FAIL", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking})
		_ = pipeStore.Advance(ctx, id, pipeline.StageFailed)
	}
	orch.RunSensorsForTest(t)
	found := false
	for _, s := range sits.Drain() {
		if s.Type == orchestrator.SitRepeatedFailure {
			found = true
		}
	}
	assert.True(t, found)
}

func TestSensors_UnmergedApproved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))
	memDB, _ := memory.Open(ctx, ":memory:")
	t.Cleanup(func() { _ = memDB.Close() })
	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, memDB)}
	orch := orchestrator.NewOrchestrator(orchestrator.Config{}, agents, checkIns, nil, nil)
	orch.SetPipeline(pipeStore)
	sits := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(sits)
	id, _ := pipeStore.Create(ctx, pipeline.WorkItem{Ticket: "JAM-APP", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved})
	_ = pipeStore.SetPRURL(ctx, id, "https://github.com/org/repo/pull/1")
	_, _ = sqlDB.ExecContext(ctx, `UPDATE work_items SET updated_at = datetime('now', '-1 hour')`)
	orch.RunSensorsForTest(t)
	found := false
	for _, s := range sits.Drain() {
		if s.Type == orchestrator.SitUnmergedApprovedPR {
			found = true
		}
	}
	assert.True(t, found)
}

func TestSensors_PipelineDrift(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))
	memDB, _ := memory.Open(ctx, ":memory:")
	t.Cleanup(func() { _ = memDB.Close() })
	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, memDB)}
	orch := orchestrator.NewOrchestrator(orchestrator.Config{}, agents, checkIns, nil, nil)
	orch.SetPipeline(pipeStore)
	sits := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(sits)
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{Agent: agent.RoleEngineer1, Status: coordination.StatusIdle, FilesTouching: []string{}}))
	id, _ := pipeStore.Create(ctx, pipeline.WorkItem{Ticket: "JAM-DRIFT", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing})
	_ = pipeStore.SetPRURL(ctx, id, "https://github.com/org/repo/pull/2")
	orch.RunSensorsForTest(t)
	found := false
	for _, s := range sits.Drain() {
		if s.Type == orchestrator.SitPipelineDrift {
			found = true
		}
	}
	assert.True(t, found)
}

// --- smartassign ---

func TestSmartAssigner_DeferExpires(t *testing.T) {
	t.Parallel()
	sa := orchestrator.NewSmartAssigner(nil)
	sa.DeferTicket("JAM-EXP", time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	assert.False(t, sa.IsDeferred("JAM-EXP"))
}

func TestSmartAssigner_SkillMatching_Backend(t *testing.T) {
	t.Parallel()
	sa := orchestrator.NewSmartAssigner(nil)
	tickets := []orchestrator.LinearTicket{{ID: "JAM-SK1", Title: "api", Labels: []string{"api", "backend"}}}
	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3})
	require.Len(t, assignments, 1)
	assert.Equal(t, agent.RoleEngineer1, assignments[0].Role)
}

func TestSmartAssigner_SkillMatching_Frontend(t *testing.T) {
	t.Parallel()
	sa := orchestrator.NewSmartAssigner(nil)
	tickets := []orchestrator.LinearTicket{{ID: "JAM-SK2", Title: "ui", Labels: []string{"frontend", "ui"}}}
	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3})
	require.Len(t, assignments, 1)
	assert.Equal(t, agent.RoleEngineer2, assignments[0].Role)
}

func TestSmartAssigner_SkillMatching_Infra(t *testing.T) {
	t.Parallel()
	sa := orchestrator.NewSmartAssigner(nil)
	tickets := []orchestrator.LinearTicket{{ID: "JAM-SK3", Title: "deploy", Labels: []string{"deploy", "infra"}}}
	assignments := sa.FilterAndRank(context.Background(), tickets, []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3})
	require.Len(t, assignments, 1)
	assert.Equal(t, agent.RoleEngineer3, assignments[0].Role)
}

func TestFormatForPM_SingleSituation(t *testing.T) {
	t.Parallel()
	situations := []orchestrator.Situation{{Type: orchestrator.SitStaleWorkingAgent, Severity: orchestrator.SeverityWarning, Description: "stale work"}}
	result := orchestrator.FormatForPM(situations)
	assert.Contains(t, result, "stale work")
	assert.Contains(t, result, "WARNING")
}

func TestFormatForPM_MultipleSeverities(t *testing.T) {
	t.Parallel()
	situations := []orchestrator.Situation{
		{Type: orchestrator.SitRepeatedFailure, Severity: orchestrator.SeverityCritical, Description: "critical"},
		{Type: orchestrator.SitPipelineDrift, Severity: orchestrator.SeverityInfo, Description: "info"},
		{Type: orchestrator.SitStaleWorkingAgent, Severity: orchestrator.SeverityWarning, Description: "warning"},
	}
	result := orchestrator.FormatForPM(situations)
	assert.Contains(t, result, "critical")
	assert.Contains(t, result, "info")
	assert.Contains(t, result, "warning")
}

// --- reconcile ---

func TestNewGHPRStateFetcher_Returns(t *testing.T) {
	t.Parallel()
	f := orchestrator.NewGHPRStateFetcher("/tmp")
	assert.NotNil(t, f)
	_, err := f(context.Background())
	assert.Error(t, err)
}

func TestReconcileWithFetcher_Reconciles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))
	id, _ := store.Create(ctx, pipeline.WorkItem{Ticket: "JAM-RF", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing})
	_ = store.SetPRURL(ctx, id, "https://github.com/org/repo/pull/77")
	orch := newReconcileOrch(t, store)
	orch.ReconcileWithFetcherForTest(ctx, func(_ context.Context) (map[string]orchestrator.PRState, error) {
		return map[string]orchestrator.PRState{"https://github.com/org/repo/pull/77": {State: "MERGED"}}, nil
	})
	item, _ := store.GetByID(ctx, id)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}

func TestReconcileWithFetcher_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))
	orch := newReconcileOrch(t, store)
	assert.NotPanics(t, func() {
		orch.ReconcileWithFetcherForTest(ctx, func(_ context.Context) (map[string]orchestrator.PRState, error) {
			return nil, fmt.Errorf("fail")
		})
	})
}

func TestFilterHealthyEngineers_NilMonitor_KeepsAll(t *testing.T) {
	t.Parallel()
	orch := orchestrator.NewOrchestrator(orchestrator.Config{}, nil, nil, nil, nil)
	assert.Len(t, orch.FilterHealthyEngineersForTest([]agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3}), 3)
}

// --- deferral ---

func TestDeferralSignal_Stop(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsDeferralSignalForTest("Stop. JAM-20 is not ready."))
}

func TestDeferralSignal_Paused(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsDeferralSignalForTest("JAM-20 is paused"))
}

func TestDeferralSignal_Wait(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsDeferralSignalForTest("Wait on JAM-20"))
}

func TestDeferralSignal_NotYet(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsDeferralSignalForTest("not yet ready for JAM-20"))
}

func TestDeferralSignal_NoMatch(t *testing.T) {
	t.Parallel()
	assert.False(t, orchestrator.ContainsDeferralSignalForTest("JAM-20 is ready to go"))
}
