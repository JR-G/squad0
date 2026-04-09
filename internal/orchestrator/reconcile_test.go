package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newReconcileOrch(t *testing.T, pipelineStore *pipeline.WorkItemStore) *orchestrator.Orchestrator {
	t.Helper()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	agents := map[agent.Role]*agent.Agent{
		agent.RolePM: buildAgent(t, runner, agent.RolePM, memDB),
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{TargetRepoDir: "/tmp/test-repo"},
		agents, checkIns, nil, nil,
	)
	orch.SetPipeline(pipelineStore)
	return orch
}

func TestReconcileItem_Merged_AdvancesPipeline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-1",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageReviewing,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/1")

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:     itemID,
		Ticket: "JAM-1",
		PRURL:  "https://github.com/org/repo/pull/1",
		Stage:  pipeline.StageReviewing,
	}, orchestrator.PRState{State: "MERGED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}

func TestReconcileItem_Closed_MarksFailed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-2",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageReviewing,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/2")

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:     itemID,
		Ticket: "JAM-2",
		PRURL:  "https://github.com/org/repo/pull/2",
		Stage:  pipeline.StageReviewing,
	}, orchestrator.PRState{State: "CLOSED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageFailed, item.Stage)
}

func TestReconcileItem_OpenApproved_AdvancesToApproved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-3",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageReviewing,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/3")

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:       itemID,
		Ticket:   "JAM-3",
		PRURL:    "https://github.com/org/repo/pull/3",
		Stage:    pipeline.StageReviewing,
		Engineer: agent.RoleEngineer1,
	}, orchestrator.PRState{State: "OPEN", ReviewDecision: "APPROVED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageApproved, item.Stage)
}

func TestReconcileItem_OpenChangesRequested_Advances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-4",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/4")

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:       itemID,
		Ticket:   "JAM-4",
		PRURL:    "https://github.com/org/repo/pull/4",
		Stage:    pipeline.StagePROpened,
		Engineer: agent.RoleEngineer1,
	}, orchestrator.PRState{State: "OPEN", ReviewDecision: "CHANGES_REQUESTED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageChangesRequested, item.Stage)
}

func TestReconcileItem_AlreadyMerged_NoOp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-5",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageMerged,
	})

	orch := newReconcileOrch(t, store)
	// Should be a no-op — already merged.
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:     itemID,
		Ticket: "JAM-5",
		Stage:  pipeline.StageMerged,
	}, orchestrator.PRState{State: "MERGED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}

func TestReconcileItem_AlreadyFailed_NoOp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-6",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageFailed,
	})

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:     itemID,
		Ticket: "JAM-6",
		Stage:  pipeline.StageFailed,
	}, orchestrator.PRState{State: "CLOSED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageFailed, item.Stage)
}

func TestParsePRStates_ValidJSON_ReturnsMap(t *testing.T) {
	t.Parallel()

	data := []byte(`[
		{"url":"https://github.com/org/repo/pull/1","state":"OPEN","reviewDecision":"APPROVED","mergeable":"MERGEABLE"},
		{"url":"https://github.com/org/repo/pull/2","state":"MERGED","reviewDecision":"","mergeable":""},
		{"url":"https://github.com/org/repo/pull/3","state":"CLOSED","reviewDecision":"CHANGES_REQUESTED","mergeable":""}
	]`)

	states, err := orchestrator.ParsePRStates(data)
	require.NoError(t, err)
	require.Len(t, states, 3)

	assert.Equal(t, "OPEN", states["https://github.com/org/repo/pull/1"].State)
	assert.Equal(t, "APPROVED", states["https://github.com/org/repo/pull/1"].ReviewDecision)
	assert.Equal(t, "MERGED", states["https://github.com/org/repo/pull/2"].State)
	assert.Equal(t, "CLOSED", states["https://github.com/org/repo/pull/3"].State)
}

func TestParsePRStates_EmptyArray_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	states, err := orchestrator.ParsePRStates([]byte(`[]`))
	require.NoError(t, err)
	assert.Empty(t, states)
}

func TestParsePRStates_InvalidJSON_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := orchestrator.ParsePRStates([]byte(`not json`))
	require.Error(t, err)
}

func TestReconcileOpen_Conflicting_PushesSituation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-7",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageReviewing,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/7")

	orch := newReconcileOrch(t, store)
	situations := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(situations)

	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:       itemID,
		Ticket:   "JAM-7",
		PRURL:    "https://github.com/org/repo/pull/7",
		Stage:    pipeline.StageReviewing,
		Engineer: agent.RoleEngineer1,
	}, orchestrator.PRState{State: "OPEN", Mergeable: "CONFLICTING"})

	drained := situations.Drain()
	require.Len(t, drained, 1)
	assert.Contains(t, drained[0].Description, "conflicts")
}

func TestReconcileWithStates_InjectedStates_Reconciles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-F", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/50")

	orch := newReconcileOrch(t, store)

	states := map[string]orchestrator.PRState{
		"https://github.com/org/repo/pull/50": {State: "MERGED"},
	}

	orch.ReconcileWithStates(ctx, states)

	item, _ := store.GetByID(ctx, itemID)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}

func TestReconcileWithStates_EmptyStates_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	orch := newReconcileOrch(t, store)

	assert.NotPanics(t, func() {
		orch.ReconcileWithStates(ctx, nil)
	})
}

func TestReconcileWithStates_FullCycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	// Create three items at different stages.
	id1, _ := store.Create(ctx, pipeline.WorkItem{Ticket: "JAM-A", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing})
	_ = store.SetPRURL(ctx, id1, "https://github.com/org/repo/pull/10")

	id2, _ := store.Create(ctx, pipeline.WorkItem{Ticket: "JAM-B", Engineer: agent.RoleEngineer2, Stage: pipeline.StagePROpened})
	_ = store.SetPRURL(ctx, id2, "https://github.com/org/repo/pull/11")

	id3, _ := store.Create(ctx, pipeline.WorkItem{Ticket: "JAM-C", Engineer: agent.RoleEngineer3, Stage: pipeline.StageReviewing})
	_ = store.SetPRURL(ctx, id3, "https://github.com/org/repo/pull/12")

	orch := newReconcileOrch(t, store)

	// Simulate GitHub states.
	ghStates := map[string]orchestrator.PRState{
		"https://github.com/org/repo/pull/10": {State: "MERGED"},
		"https://github.com/org/repo/pull/11": {State: "CLOSED"},
		"https://github.com/org/repo/pull/12": {State: "OPEN", ReviewDecision: "APPROVED"},
	}

	orch.ReconcileWithStates(ctx, ghStates)

	item1, _ := store.GetByID(ctx, id1)
	assert.Equal(t, pipeline.StageMerged, item1.Stage)

	item2, _ := store.GetByID(ctx, id2)
	assert.Equal(t, pipeline.StageFailed, item2.Stage)

	item3, _ := store.GetByID(ctx, id3)
	assert.Equal(t, pipeline.StageApproved, item3.Stage)
}

func TestReconcileWithStates_NoPipeline_DoesNotPanic(t *testing.T) {
	t.Parallel()

	orch := orchestrator.NewOrchestrator(orchestrator.Config{}, nil, nil, nil, nil)
	assert.NotPanics(t, func() {
		orch.ReconcileWithStates(context.Background(), map[string]orchestrator.PRState{})
	})
}

func TestReconcileWithStates_UnknownPR_Skipped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{Ticket: "JAM-X", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/99")

	orch := newReconcileOrch(t, store)

	// GitHub state map doesn't include this PR — should be skipped.
	orch.ReconcileWithStates(ctx, map[string]orchestrator.PRState{
		"https://github.com/org/repo/pull/1": {State: "MERGED"},
	})

	item, _ := store.GetByID(ctx, itemID)
	assert.Equal(t, pipeline.StageReviewing, item.Stage, "unknown PR should not be modified")
}
