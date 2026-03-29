package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetProjectFactStore_SetsStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	factStore := memory.NewFactStore(memDB)
	// Should not panic.
	orch.SetProjectFactStore(factStore)
}

func TestWriteHandoff_NilStore_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	// No handoff store set — should be a no-op.
	orch.WriteHandoffForTest(context.Background(), "JAM-1", agent.RoleEngineer1, "completed", "did stuff", "feat/JAM-1")
}

func TestWriteHandoff_WithStore_PersistsHandoff(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	checkIns := coordination.NewCheckInStore(sqlDB)
	handoffStore := pipeline.NewHandoffStore(sqlDB)
	require.NoError(t, handoffStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetHandoffStore(handoffStore)

	orch.WriteHandoffForTest(ctx, "JAM-5", agent.RoleEngineer1, "completed", "implementation done", "feat/JAM-5")

	handoff, err := handoffStore.LatestForTicket(ctx, "JAM-5")
	require.NoError(t, err)
	assert.Equal(t, "JAM-5", handoff.Ticket)
	assert.Equal(t, "completed", handoff.Status)
	assert.Equal(t, "clean", handoff.GitState)
}

func TestWriteHandoff_FailedStatus_SetsDirtyGitState(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	checkIns := coordination.NewCheckInStore(sqlDB)
	handoffStore := pipeline.NewHandoffStore(sqlDB)
	require.NoError(t, handoffStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetHandoffStore(handoffStore)

	orch.WriteHandoffForTest(ctx, "JAM-6", agent.RoleEngineer2, "failed", "crash", "feat/JAM-6")

	handoff, err := handoffStore.LatestForTicket(ctx, "JAM-6")
	require.NoError(t, err)
	assert.Equal(t, "dirty", handoff.GitState)
}

func TestRosterContext_EmptyRoster_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.RosterContextForTest(nil))
	assert.Empty(t, orchestrator.RosterContextForTest(map[agent.Role]string{}))
}

func TestRosterContext_WithNames_FormatsCorrectly(t *testing.T) {
	t.Parallel()
	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Cormac",
		agent.RoleEngineer2: "Callum",
	}
	result := orchestrator.RosterContextForTest(roster)
	assert.Contains(t, result, "Cormac")
	assert.Contains(t, result, "Callum")
	assert.Contains(t, result, "Team names:")
}

func TestRosterContext_NamesMatchRole_Excluded(t *testing.T) {
	t.Parallel()
	// When name equals role ID, it should be excluded.
	roster := map[agent.Role]string{
		agent.RoleEngineer1: string(agent.RoleEngineer1),
	}
	assert.Empty(t, orchestrator.RosterContextForTest(roster))
}

func TestAnnounceSessionResult_WithPR_SetsPipeline(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	checkIns := coordination.NewCheckInStore(sqlDB)
	pipeStore := newPipelineStore(t, sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-10", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	orch.AnnounceSessionResultForTest(ctx, "https://github.com/test/repo/pull/1", "JAM-10", itemID, agent.RoleEngineer1)

	item, err := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/test/repo/pull/1", item.PRURL)
}

func TestAnnounceSessionResult_NoPR_MarksFailed(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	checkIns := coordination.NewCheckInStore(sqlDB)
	pipeStore := newPipelineStore(t, sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-11", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	orch.AnnounceSessionResultForTest(ctx, "", "JAM-11", itemID, agent.RoleEngineer1)

	item, err := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.StageFailed, item.Stage)
}

func TestFailAndRequeue_MarksFailedAndMovesTicket(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	checkIns := coordination.NewCheckInStore(sqlDB)
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-FAIL", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	orch.FailAndRequeueForTest(ctx, pipeline.WorkItem{ID: itemID, Ticket: "JAM-FAIL"})

	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageFailed, item.Stage)

	// PM should have been called to move ticket to Todo.
	pmRunner.mu.Lock()
	defer pmRunner.mu.Unlock()
	assert.GreaterOrEqual(t, len(pmRunner.calls), 1, "PM should move ticket to Todo")
}

func TestResumeStaleWorkItem_CurrentSession_LeftAlone(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	checkIns := coordination.NewCheckInStore(sqlDB)
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	// Create an item — it will have UpdatedAt ~now, after startedAt.
	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-CURR", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	// Run briefly — the item should NOT be failed because it's from this session.
	timedCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)

	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageWorking, item.Stage, "current-session item should not be failed")
}

func TestRecordSessionStart_WithMonitor_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	monitor := health.NewMonitor([]agent.Role{agent.RoleEngineer1}, health.MonitorConfig{})

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetHealthMonitor(monitor)

	// Exercise recordSessionStart/End via the exported session lifecycle.
	// The monitor won't panic even with unknown roles.
	monitor.RecordSessionStart(agent.RoleEngineer1)
	monitor.RecordSessionEnd(agent.RoleEngineer1, "JAM-1", true)

	agentHealth, err := monitor.GetHealth(agent.RoleEngineer1)
	require.NoError(t, err)
	assert.NotEmpty(t, agentHealth.State)
}
