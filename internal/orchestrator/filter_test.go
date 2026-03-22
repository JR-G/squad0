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

func TestOrchestrator_Run_Tick_PMError_ContinuesLoop(t *testing.T) {
	t.Parallel()

	// PM returns an error on assignment request. The tick should log and
	// continue without crashing.
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"rate limited"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}

	orch, _ := setupOrchestratorWithEngineers(t, pmRunner, map[agent.Role]*fakeProcessRunner{
		agent.RoleEngineer1: engRunner,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := orch.Run(ctx)

	// Should exit cleanly via context cancellation, not crash.
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestOrchestrator_Run_Tick_OnlyNonEngineerRolesIdle_SkipsTick(t *testing.T) {
	t.Parallel()

	// Set up orchestrator with PM and a non-engineer role only.
	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	checkIns := coordination.NewCheckInStore(db)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

	// Only PM and Designer in the agent map — no engineers.
	agents := map[agent.Role]*agent.Agent{
		agent.RolePM: pmAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	runErr := orch.Run(ctx)

	// Should complete without errors — PM is idle but not an engineer, so
	// no assignment is requested.
	assert.ErrorIs(t, runErr, context.DeadlineExceeded)
}

func TestOrchestrator_Run_MultipleEngineersAssigned(t *testing.T) {
	t.Parallel()

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-1","description":"Task A"},{"role":"engineer-2","ticket":"SQ-2","description":"Task B"}]`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	pmRunner := &fakeProcessRunner{output: []byte(pmOutput)}
	eng1Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done A"}` + "\n")}
	eng2Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done B"}` + "\n")}

	orch, _ := setupOrchestratorWithEngineers(t, pmRunner, map[agent.Role]*fakeProcessRunner{
		agent.RoleEngineer1: eng1Runner,
		agent.RoleEngineer2: eng2Runner,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	// Orchestrator should return cleanly without panic, even with
	// multiple concurrent goroutines completing sessions.
	assert.False(t, orch.IsRunning())
}

func TestOrchestrator_Run_SessionWithTranscript_PostsFinished(t *testing.T) {
	t.Parallel()

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-42","description":"Add caching"}]`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	pmRunner := &fakeProcessRunner{output: []byte(pmOutput)}
	// Return a result with non-empty transcript content.
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Implemented caching layer"}` + "\n"),
	}

	orch, _ := setupOrchestratorWithEngineers(t, pmRunner, map[agent.Role]*fakeProcessRunner{
		agent.RoleEngineer1: engRunner,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	// No bot configured so postAsRole is a no-op, but the code path
	// through runSession is exercised without error.
	assert.False(t, orch.IsRunning())
}

func TestOrchestrator_PostAsRole_NilBot_DoesNotPanic(t *testing.T) {
	t.Parallel()

	// This test verifies that the nil bot guard in postAsRole works.
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run triggers postAsRole with a nil bot on startup message.
	err := orch.Run(ctx)

	// Should complete without panic.
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
