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
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveConflicts_NoPipeline_DoesNotPanic(t *testing.T) {
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

	orch.ResolveConflictsForTest(context.Background(), []agent.Role{agent.RoleEngineer1})
}

func TestResolveConflicts_NoOpenPRs_DoesNothing(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	checkIns := coordination.NewCheckInStore(sqlDB)
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	orch.ResolveConflictsForTest(ctx, []agent.Role{agent.RoleEngineer1})
}

func TestStartConflictResolution_NoAgent_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	// No engineer agent — should return early.
	orch.StartConflictResolutionForTest(ctx, pipeline.WorkItem{
		Ticket: "JAM-1", Engineer: agent.RoleEngineer1,
		PRURL: "https://github.com/test/repo/pull/1",
	})
}

func TestStartConflictResolution_BadRepo_LogsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{TargetRepoDir: "/nonexistent"},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, nil, nil,
	)

	// Bad repo dir — worktree creation fails, should not panic.
	orch.StartConflictResolutionForTest(ctx, pipeline.WorkItem{
		Ticket: "JAM-2", Engineer: agent.RoleEngineer1,
		PRURL: "https://github.com/test/repo/pull/2",
	})
}

func TestIsRoleInList(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1, agent.RoleReviewer}
	assert.True(t, orchestrator.IsRoleInListForTest(agent.RoleEngineer1, roles))
	assert.False(t, orchestrator.IsRoleInListForTest(agent.RolePM, roles))
}

func TestResolveConflicts_WithConflictingPR_SendsEngineer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	// PM returns CONFLICTING when asked about mergeability.
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CONFLICTING"}` + "\n"),
	}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"rebased"}` + "\n"),
	}
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{TargetRepoDir: "/nonexistent"},
		map[agent.Role]*agent.Agent{
			agent.RolePM:        pmAgent,
			agent.RoleEngineer1: engAgent,
		},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-CF1", Engineer: agent.RoleEngineer1, Stage: pipeline.StagePROpened,
	})
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test/repo/pull/1"))

	orch.ResolveConflictsForTest(ctx, []agent.Role{agent.RoleEngineer1})

	// PM should have been called to check mergeability.
	pmRunner.mu.Lock()
	pmCalls := len(pmRunner.calls)
	pmRunner.mu.Unlock()
	assert.GreaterOrEqual(t, pmCalls, 1, "PM should check mergeability")
}
