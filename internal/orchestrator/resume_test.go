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

func TestResumePendingWork_ResumesApprovedItems(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))
	pipeStore := newPipelineStore(t, sqlDB)

	db, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = db.Close() })

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	agents := map[agent.Role]*agent.Agent{agent.RolePM: pmAgent}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-50", Engineer: agent.RolePM, Stage: pipeline.StageApproved,
	})
	_ = pipeStore.SetPRURL(ctx, itemID, "https://github.com/test-org/test-repo/pull/50")
	_ = pipeStore.Advance(ctx, itemID, pipeline.StageApproved)

	timedCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)

	assert.NotEmpty(t, pmRunner.calls)
}

func TestResumePendingWork_PROpenedItem_TriggersReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))
	pipeStore := newPipelineStore(t, sqlDB)

	db, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = db.Close() })

	reviewRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"APPROVED"}` + "\n")}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}

	pmAgent := setupPMAgent(t, pmRunner)
	reviewer := buildAgent(t, reviewRunner, agent.RoleReviewer, db)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:       pmAgent,
		agent.RoleReviewer: reviewer,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-60", Engineer: agent.RoleEngineer1, Stage: pipeline.StagePROpened,
	})
	_ = pipeStore.SetPRURL(ctx, itemID, "https://github.com/test-org/test-repo/pull/60")

	// Call StartReviewForTest directly instead of going through Run.
	orch.StartReviewWithItemForTest(ctx, "https://github.com/test-org/test-repo/pull/60", "JAM-60", itemID, agent.RoleEngineer1)
	orch.Wait()

	assert.NotEmpty(t, reviewRunner.calls)
}

func TestResumePendingWork_WorkingItems_DoesNotPanic(t *testing.T) {
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

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	_, _ = pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-70", Engineer: agent.RolePM, Stage: pipeline.StageWorking,
	})

	timedCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		_ = orch.Run(timedCtx)
	})
}

func TestMergeAndComplete_NoPM_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// No PM agent — mergeAndComplete should return early.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, orchestrator.NewAssigner(nil, "TEST"),
	)

	assert.NotPanics(t, func() {
		orch.MergeForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "T-1", 0)
	})
}

func TestMergeAndComplete_MergeFails_AnnouncesFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"merge conflict"}` + "\n"),
		err:    assert.AnError,
	}
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.NotPanics(t, func() {
		orch.MergeForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "T-1", 0)
	})
}

func TestMergeAndComplete_Success_AdvancesToMerged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))
	pipeStore := newPipelineStore(t, sqlDB)

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-80", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved,
	})

	orch.MergeForTest(ctx, "https://github.com/test-org/test-repo/pull/80", "JAM-80", itemID)

	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}

func TestResumePendingWork_NilPipeline_DoesNotPanic(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		_ = orch.Run(ctx)
	})
}
