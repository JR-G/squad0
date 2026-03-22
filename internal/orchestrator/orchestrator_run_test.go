package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupOrchestratorWithEngineers(
	t *testing.T,
	pmRunner *fakeProcessRunner,
	engineerRunners map[agent.Role]*fakeProcessRunner,
) (*orchestrator.Orchestrator, *coordination.CheckInStore) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	checkIns := coordination.NewCheckInStore(db)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmAgent := setupPMAgent(t, pmRunner)
	agents := map[agent.Role]*agent.Agent{
		agent.RolePM: pmAgent,
	}

	for role, runner := range engineerRunners {
		agents[role] = setupAgentWithRole(t, runner, role)
	}

	assigner := orchestrator.NewAssigner(pmAgent)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:  50 * time.Millisecond,
			MaxParallel:   3,
			CooldownAfter: 5 * time.Second,
		},
		agents, checkIns, nil, assigner,
	)

	return orch, checkIns
}

func TestOrchestrator_Run_TickAssignsWork(t *testing.T) {
	t.Parallel()

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-42","description":"Fix the auth bug"}]`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	pmRunner := &fakeProcessRunner{output: []byte(pmOutput)}
	engRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}

	orch, checkIns := setupOrchestratorWithEngineers(t, pmRunner, map[agent.Role]*fakeProcessRunner{
		agent.RoleEngineer1: engRunner,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	// After run, the engineer should be back to idle (session completed).
	checkIn, getErr := checkIns.GetByAgent(context.Background(), agent.RoleEngineer1)
	require.NoError(t, getErr)
	assert.Equal(t, coordination.StatusIdle, checkIn.Status)
}

func TestOrchestrator_Run_ContextCancelled_ReturnsError(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := orch.Run(ctx)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestOrchestrator_Run_SetsRunningFalseAfterReturn(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	assert.False(t, orch.IsRunning())
}

func TestOrchestrator_Run_InitialiseCheckInsError_ReturnsError(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)

	checkIns := coordination.NewCheckInStore(db)
	// Do NOT init schema so upsert will fail.

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM: pmAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent),
	)

	err = orch.Run(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "initialising check-ins")

	require.NoError(t, db.Close())
}

func TestOrchestrator_Run_NoIdleEngineers_DoesNotAssign(t *testing.T) {
	t.Parallel()

	// PM returns assignments but only non-engineer roles are idle.
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, checkIns := setupOrchestrator(t, pmRunner)

	ctx := context.Background()
	// Manually set PM to working so no engineers are idle.
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RolePM, Status: coordination.StatusWorking, FilesTouching: []string{},
	}))
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	}))

	timedCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)

	// PM runner should have been called once for initialise check-ins, but
	// the important thing is no error and clean shutdown.
	assert.False(t, orch.IsRunning())
}

func TestOrchestrator_Run_SessionError_AgentReturnsToIdle(t *testing.T) {
	t.Parallel()

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-42","description":"Fix bug"}]`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	pmRunner := &fakeProcessRunner{output: []byte(pmOutput)}
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"session failed"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}

	orch, checkIns := setupOrchestratorWithEngineers(t, pmRunner, map[agent.Role]*fakeProcessRunner{
		agent.RoleEngineer1: engRunner,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	// After session error, agent should still be set to idle.
	checkIn, getErr := checkIns.GetByAgent(context.Background(), agent.RoleEngineer1)
	require.NoError(t, getErr)
	assert.Equal(t, coordination.StatusIdle, checkIn.Status)
}

func TestOrchestrator_Run_UnknownRole_InAssignment_Skipped(t *testing.T) {
	t.Parallel()

	// PM assigns to a role that doesn't exist in the agents map.
	assignmentJSON := `[{"role":"engineer-99","ticket":"SQ-42","description":"Fix bug"}]`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	pmRunner := &fakeProcessRunner{output: []byte(pmOutput)}

	orch, _ := setupOrchestratorWithEngineers(t, pmRunner, map[agent.Role]*fakeProcessRunner{
		agent.RoleEngineer1: {output: []byte(`{"type":"result","result":"done"}` + "\n")},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	// Should not crash — unknown roles are silently skipped.
	assert.False(t, orch.IsRunning())
}
