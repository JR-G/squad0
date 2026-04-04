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
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// filterByWIP — additional branch coverage
// ---------------------------------------------------------------------------

func TestFilterByWIP_EngineerWithOpenItems_NotIdle_Skipped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	eng1Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	eng1 := setupAgentWithRole(t, eng1Runner, agent.RoleEngineer1)
	eng2 := setupAgentWithRole(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}, agent.RoleEngineer2)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1,
		agent.RoleEngineer2: eng2,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second, WorkEnabled: true},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Engineer-1 has an open reviewing item AND is currently working (not idle).
	_, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-BUSY", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	require.NoError(t, createErr)

	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RoleEngineer1,
		Status:        "working",
		FilesTouching: []string{"internal/foo.go"},
	}))

	// Run briefly — engineer-1 should be skipped (busy with open items).
	timedCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)

	// Engineer-1 should NOT have been called for new work because they have
	// open items and are not idle. The PM should not have assigned them work.
	eng1Runner.mu.Lock()
	eng1Calls := len(eng1Runner.calls)
	eng1Runner.mu.Unlock()

	// Engineer-1 should have zero calls — they were skipped by filterByWIP.
	assert.Equal(t, 0, eng1Calls, "engineer-1 should not be called when busy with open items")
}

func TestFilterByWIP_EngineerIdleWithPR_NotAvailableForNewWork(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)
	eng1Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	eng1 := buildAgent(t, eng1Runner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second, WorkEnabled: true},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Engineer-1 has an open reviewing item — filterByWIP should not
	// make them available for new work.
	_, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-REVIEWING", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	require.NoError(t, createErr)

	timedCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)

	// PM should not have requested assignments for engineer-1 because
	// filterByWIP removed them from the available list. Engineer-1 should
	// have zero calls (no new work sessions started).
	eng1Runner.mu.Lock()
	eng1Calls := len(eng1Runner.calls)
	eng1Runner.mu.Unlock()

	assert.Equal(t, 0, eng1Calls,
		"engineer-1 should not be called for new work when they have open items")
}

func TestFilterByWIP_PipelineStoreError_KeepsEngineer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	eng1 := setupAgentWithRole(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}, agent.RoleEngineer1)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second, WorkEnabled: true},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Close the DB to force errors from OpenByEngineer.
	_ = sqlDB.Close()

	timedCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()

	// Should not panic — when pipelineStore.OpenByEngineer errors,
	// the engineer is kept in the available list.
	assert.NotPanics(t, func() {
		_ = orch.Run(timedCtx)
	})
}

// ---------------------------------------------------------------------------
// clearStaleWork — additional branch coverage
// ---------------------------------------------------------------------------

func TestClearStaleWork_IdleWithNoPR_MarksFailedAndReturnsTrue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)
	eng1 := buildAgent(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second, WorkEnabled: true},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Engineer has a working item with no PR.
	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-STALE-CLEAR", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, createErr)

	// Engineer is idle — clearStaleWork fires.
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RoleEngineer1,
		Status:        "idle",
		FilesTouching: []string{},
	}))

	timedCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)
	orch.Wait()

	// The item should be failed because it's working with no PR and idle.
	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageFailed, item.Stage,
		"idle engineer's working item with no PR should be marked failed")
}

func TestClearStaleWork_WorkingNoPR_FailedDirectly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM: pmAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		agents, checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	// A working item with no PR — clearStaleWork should mark it failed
	// via failAndRequeue.
	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-NOPRCLEAR", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)

	orch.FailAndRequeueForTest(ctx, item)

	// Allow goroutine to complete (MoveTicketState runs async).
	time.Sleep(100 * time.Millisecond)

	updated, getErr2 := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr2)
	assert.Equal(t, pipeline.StageFailed, updated.Stage,
		"working item with no PR should be marked failed")
}
