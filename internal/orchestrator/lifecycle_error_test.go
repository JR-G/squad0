package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupClosedDBOrch(t *testing.T) *orchestrator.Orchestrator {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)

	checkIns := coordination.NewCheckInStore(db)
	require.NoError(t, checkIns.InitSchema(context.Background()))
	require.NoError(t, db.Close())

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","content":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: setupEngineerAgent(t, agent.RoleEngineer1),
	}

	return orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent),
	)
}

func TestPauseAgent_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	orch := setupClosedDBOrch(t)

	err := orch.PauseAgent(context.Background(), agent.RoleEngineer1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pausing")
}

func TestResumeAgent_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	orch := setupClosedDBOrch(t)

	err := orch.ResumeAgent(context.Background(), agent.RoleEngineer1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resuming")
}

func TestPauseAll_ErrorOnFirstAgent_ReturnsError(t *testing.T) {
	t.Parallel()

	orch := setupClosedDBOrch(t)

	err := orch.PauseAll(context.Background())

	require.Error(t, err)
}

func TestResumeAll_ErrorOnFirstAgent_ReturnsError(t *testing.T) {
	t.Parallel()

	orch := setupClosedDBOrch(t)

	err := orch.ResumeAll(context.Background())

	require.Error(t, err)
}

func TestStatus_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	orch := setupClosedDBOrch(t)

	_, err := orch.Status(context.Background())

	require.Error(t, err)
}

func TestPauseAgent_SetsMessage(t *testing.T) {
	t.Parallel()

	orch, checkIns := setupLifecycleOrch(t)
	ctx := context.Background()

	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer2, Status: coordination.StatusWorking,
		FilesTouching: []string{"handler.go"},
		Message:       "implementing feature",
	}))

	err := orch.PauseAgent(ctx, agent.RoleEngineer2)

	require.NoError(t, err)
	checkIn, getErr := checkIns.GetByAgent(ctx, agent.RoleEngineer2)
	require.NoError(t, getErr)
	assert.Equal(t, "paused by CEO", checkIn.Message)
	assert.Equal(t, coordination.StatusIdle, checkIn.Status)
}

func TestResumeAgent_ClearsMessage(t *testing.T) {
	t.Parallel()

	orch, checkIns := setupLifecycleOrch(t)
	ctx := context.Background()

	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer2, Status: coordination.StatusIdle,
		FilesTouching: []string{},
		Message:       "paused by CEO",
	}))

	err := orch.ResumeAgent(ctx, agent.RoleEngineer2)

	require.NoError(t, err)
	checkIn, getErr := checkIns.GetByAgent(ctx, agent.RoleEngineer2)
	require.NoError(t, getErr)
	assert.Empty(t, checkIn.Message)
}

func TestStatus_ReturnsMultipleCheckIns(t *testing.T) {
	t.Parallel()

	orch, checkIns := setupLifecycleOrch(t)
	ctx := context.Background()

	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RolePM, Status: coordination.StatusIdle, FilesTouching: []string{},
	}))
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	}))
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer2, Status: coordination.StatusBlocked, FilesTouching: []string{},
	}))

	status, err := orch.Status(ctx)

	require.NoError(t, err)
	assert.Len(t, status, 3)
}
