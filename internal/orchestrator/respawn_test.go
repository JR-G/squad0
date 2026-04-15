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

func TestIsDeadWorkingSession_WorkingWithNoPR_True(t *testing.T) {
	t.Parallel()
	item := pipeline.WorkItem{Stage: pipeline.StageWorking}
	assert.True(t, orchestrator.IsDeadWorkingSessionForTest(item))
}

func TestIsDeadWorkingSession_WithPR_False(t *testing.T) {
	t.Parallel()
	item := pipeline.WorkItem{
		Stage: pipeline.StageWorking,
		PRURL: "https://github.com/org/repo/pull/1",
	}
	assert.False(t, orchestrator.IsDeadWorkingSessionForTest(item))
}

func TestIsDeadWorkingSession_NotWorking_False(t *testing.T) {
	t.Parallel()
	item := pipeline.WorkItem{Stage: pipeline.StageReviewing}
	assert.False(t, orchestrator.IsDeadWorkingSessionForTest(item))
}

func TestHandleDeadSession_InsideGrace_DoesNotRespawn(t *testing.T) {
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

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"work"}` + "\n"),
	}
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{TargetRepoDir: t.TempDir()},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	item := pipeline.WorkItem{
		ID:        42,
		Ticket:    "JAM-GRACE",
		Engineer:  agent.RoleEngineer1,
		Stage:     pipeline.StageWorking,
		UpdatedAt: time.Now(), // fresh — inside grace
	}

	orch.HandleDeadSessionForTest(ctx, agent.RoleEngineer1, item)
	orch.Wait()

	// Inside grace: should NOT have respawned (no engineer call).
	engRunner.mu.Lock()
	calls := len(engRunner.calls)
	engRunner.mu.Unlock()
	assert.Equal(t, 0, calls, "respawn should wait inside the grace window")
}

func TestHandleDeadSession_BeyondGrace_Respawns(t *testing.T) {
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

	repoDir := t.TempDir()
	initTestRepoWithBranch(t, repoDir, "feat/jam-respawn")

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"resumed work"}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}

	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{TargetRepoDir: repoDir, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{
			agent.RolePM:        pmAgent,
			agent.RoleEngineer1: engAgent,
		},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-RESPAWN",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-respawn",
	})
	require.NoError(t, createErr)

	// Age the item past the grace window.
	_, err = sqlDB.ExecContext(ctx,
		`UPDATE work_items SET updated_at = datetime('now', '-5 minutes') WHERE id = ?`, itemID)
	require.NoError(t, err)

	item, err := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, err)

	orch.HandleDeadSessionForTest(ctx, agent.RoleEngineer1, item)
	orch.Wait()

	// Beyond grace: should have respawned the engineer session.
	engRunner.mu.Lock()
	calls := len(engRunner.calls)
	engRunner.mu.Unlock()
	assert.GreaterOrEqual(t, calls, 1, "respawn should execute beyond the grace window")
}

func TestResumeAssignment_NoAgentForRole_NoOp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{}, // no agents
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	item := pipeline.WorkItem{
		ID:       1,
		Ticket:   "JAM-NOAG",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageAssigned,
	}

	// Should return immediately without panicking.
	orch.ResumeAssignmentForTest(ctx, item)
	orch.Wait()
}

func TestAgent_Name_WithRoster_ReturnsChosenName(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{}
	engAgent := agent.NewAgent(agent.RoleEngineer1, "claude-sonnet-4-6", agent.NewSession(runner), nil, nil, nil, nil, nil)
	engAgent.SetChatContext(map[agent.Role]string{agent.RoleEngineer1: "Callum"}, nil, "")

	assert.Equal(t, "Callum", engAgent.Name())
}

func TestAgent_Name_EmptyRoster_FallsBackToRoleSlug(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{}
	pmAgent := agent.NewAgent(agent.RolePM, "claude-sonnet-4-6", agent.NewSession(runner), nil, nil, nil, nil, nil)

	assert.Equal(t, "pm", pmAgent.Name())
}
