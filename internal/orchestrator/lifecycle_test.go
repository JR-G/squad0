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

func setupLifecycleOrch(t *testing.T) (*orchestrator.Orchestrator, *coordination.CheckInStore) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	checkIns := coordination.NewCheckInStore(db)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","content":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: setupEngineerAgent(t, agent.RoleEngineer1),
		agent.RoleEngineer2: setupEngineerAgent(t, agent.RoleEngineer2),
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent),
	)

	return orch, checkIns
}

func TestPauseAgent_SetsIdle(t *testing.T) {
	t.Parallel()

	orch, checkIns := setupLifecycleOrch(t)
	ctx := context.Background()

	_ = checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	})

	err := orch.PauseAgent(ctx, agent.RoleEngineer1)

	require.NoError(t, err)
	checkIn, err := checkIns.GetByAgent(ctx, agent.RoleEngineer1)
	require.NoError(t, err)
	assert.Equal(t, coordination.StatusIdle, checkIn.Status)
	assert.Contains(t, checkIn.Message, "paused")
}

func TestResumeAgent_SetsIdle(t *testing.T) {
	t.Parallel()

	orch, checkIns := setupLifecycleOrch(t)
	ctx := context.Background()

	_ = checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusIdle, FilesTouching: []string{},
		Message: "paused by CEO",
	})

	err := orch.ResumeAgent(ctx, agent.RoleEngineer1)

	require.NoError(t, err)
	checkIn, err := checkIns.GetByAgent(ctx, agent.RoleEngineer1)
	require.NoError(t, err)
	assert.Equal(t, coordination.StatusIdle, checkIn.Status)
}

func TestPauseAll_PausesEveryAgent(t *testing.T) {
	t.Parallel()

	orch, checkIns := setupLifecycleOrch(t)
	ctx := context.Background()

	for _, role := range []agent.Role{agent.RolePM, agent.RoleEngineer1, agent.RoleEngineer2} {
		_ = checkIns.Upsert(ctx, coordination.CheckIn{
			Agent: role, Status: coordination.StatusWorking, FilesTouching: []string{},
		})
	}

	err := orch.PauseAll(ctx)

	require.NoError(t, err)
	allCheckIns, _ := checkIns.GetAll(ctx)
	for _, checkIn := range allCheckIns {
		assert.Equal(t, coordination.StatusIdle, checkIn.Status)
	}
}

func TestResumeAll_ResumesEveryAgent(t *testing.T) {
	t.Parallel()

	orch, checkIns := setupLifecycleOrch(t)
	ctx := context.Background()

	for _, role := range []agent.Role{agent.RolePM, agent.RoleEngineer1, agent.RoleEngineer2} {
		_ = checkIns.Upsert(ctx, coordination.CheckIn{
			Agent: role, Status: coordination.StatusIdle, FilesTouching: []string{},
			Message: "paused",
		})
	}

	err := orch.ResumeAll(ctx)

	require.NoError(t, err)
}
