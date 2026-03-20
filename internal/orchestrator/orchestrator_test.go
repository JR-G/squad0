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

func setupOrchestrator(t *testing.T, pmRunner *fakeProcessRunner) (*orchestrator.Orchestrator, *coordination.CheckInStore) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	checkIns := coordination.NewCheckInStore(db)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmAgent := setupPMAgent(t, pmRunner)
	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: setupEngineerAgent(t, agent.RoleEngineer1),
	}

	assigner := orchestrator.NewAssigner(pmAgent)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:  100 * time.Millisecond,
			MaxParallel:   3,
			CooldownAfter: 5 * time.Second,
		},
		agents,
		checkIns,
		nil,
		assigner,
	)

	return orch, checkIns
}

func setupEngineerAgent(t *testing.T, role agent.Role) *agent.Agent {
	t.Helper()
	runner := &fakeProcessRunner{output: []byte(`{"type":"result","content":"done"}` + "\n")}
	return setupAgentWithRole(t, runner, role)
}

func setupAgentWithRole(t *testing.T, runner *fakeProcessRunner, role agent.Role) *agent.Agent {
	t.Helper()

	ctx := context.Background()

	memDB, err := openMemoryDB(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	return buildAgent(t, runner, role, memDB)
}

func TestOrchestrator_IsRunning_DefaultFalse(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","content":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)

	assert.False(t, orch.IsRunning())
}

func TestOrchestrator_Run_SetsRunningTrue(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","content":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	assert.False(t, orch.IsRunning())
}

func TestOrchestrator_Run_InitialisesCheckIns(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","content":"[]"}` + "\n")}
	orch, checkIns := setupOrchestrator(t, pmRunner)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	allCheckIns, err := checkIns.GetAll(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, allCheckIns)
}

func TestOrchestrator_Status_ReturnsCheckIns(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","content":"[]"}` + "\n")}
	orch, checkIns := setupOrchestrator(t, pmRunner)
	ctx := context.Background()

	_ = checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	})

	status, err := orch.Status(ctx)

	require.NoError(t, err)
	assert.Len(t, status, 1)
}
