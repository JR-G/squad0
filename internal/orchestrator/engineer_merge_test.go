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

func TestStartEngineerMerge_EngineerMerges_AdvancesPipeline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// Engineer session runs the merge, PM verifies merged.
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Merged. done."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"MERGED"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-EM1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved, Branch: "feat/jam-em1",
	})
	require.NoError(t, createErr)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/100", "JAM-EM1", itemID, agent.RoleEngineer1)

	// Pipeline should have advanced to merged.
	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageMerged, item.Stage)

	// Engineer should have been called with the merge prompt.
	engRunner.mu.Lock()
	engCalls := len(engRunner.calls)
	engRunner.mu.Unlock()
	assert.GreaterOrEqual(t, engCalls, 1, "engineer should have run merge session")
}

func TestStartEngineerMerge_EngineerSetsCheckIn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Merged."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"MERGED"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)

	orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/101", "JAM-EM2", 0, agent.RoleEngineer1)

	// After merge, engineer should be back to idle.
	checkIn, getErr := checkIns.GetByAgent(ctx, agent.RoleEngineer1)
	require.NoError(t, getErr)
	assert.Equal(t, coordination.StatusIdle, checkIn.Status)
}

func TestStartEngineerMerge_VerifyFails_AnnouncesFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// Engineer merges but verification says OPEN (not merged).
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"OPEN"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-EM3", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved, Branch: "feat/jam-em3",
	})
	require.NoError(t, createErr)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/102", "JAM-EM3", itemID, agent.RoleEngineer1)

	// Pipeline should NOT have advanced to merged.
	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.NotEqual(t, pipeline.StageMerged, item.Stage)
}

func TestStartEngineerMerge_SessionError_AnnouncesFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"session crashed"}` + "\n"),
		err:    assert.AnError,
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/103", "JAM-EM4", 0, agent.RoleEngineer1)
	})
}

func TestStartEngineerMerge_NoEngineer_FallsBackToPM(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// PM: checkApprovalStatus → APPROVED, executeMerge → done, verifyMerged → MERGED.
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
		outputs: [][]byte{
			[]byte(`{"type":"result","result":"APPROVED"}` + "\n"),
			[]byte(`{"type":"result","result":"done"}` + "\n"),
			[]byte(`{"type":"result","result":"MERGED"}` + "\n"),
		},
	}
	pmAgent := setupPMAgent(t, pmRunner)

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-EM5", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved, Branch: "feat/jam-em5",
	})
	require.NoError(t, createErr)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// No engineer-1 in agents — should fall back to PM.
	orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/104", "JAM-EM5", itemID, agent.RoleEngineer1)

	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageMerged, item.Stage)

	// PM should have been called for the merge.
	pmRunner.mu.Lock()
	pmCalls := len(pmRunner.calls)
	pmRunner.mu.Unlock()
	assert.GreaterOrEqual(t, pmCalls, 1)
}

func TestStartEngineerMerge_NoPMNoEngineer_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/105", "JAM-EM6", 0, agent.RoleEngineer1)
	})
}

func TestStartEngineerMerge_PromptContainsMergeInstructions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"MERGED"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)

	orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/106", "JAM-EM7", 0, agent.RoleEngineer1)

	engRunner.mu.Lock()
	defer engRunner.mu.Unlock()
	require.NotEmpty(t, engRunner.calls)
	assert.Contains(t, engRunner.calls[0].stdin, "gh pr merge https://github.com/test-org/test-repo/pull/106 --squash --delete-branch")
	assert.Contains(t, engRunner.calls[0].stdin, "gh pr checks https://github.com/test-org/test-repo/pull/106")
	assert.Contains(t, engRunner.calls[0].stdin, "JAM-EM7")
}
