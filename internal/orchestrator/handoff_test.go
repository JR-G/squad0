package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHandoffOrch(t *testing.T, engRunner *fakeProcessRunner) (*orchestrator.Orchestrator, *pipeline.HandoffStore) {
	t.Helper()

	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	handoffStore := pipeline.NewHandoffStore(sqlDB)
	require.NoError(t, handoffStore.InitSchema(ctx))

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := setupAgentWithRole(t, engRunner, agent.RoleEngineer1)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      1,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)
	orch.SetHandoffStore(handoffStore)

	return orch, handoffStore
}

func TestBuildHandoffContext_NoStore_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := orchestrator.BuildHandoffContext(context.Background(), nil, "JAM-1")

	assert.Empty(t, result)
}

func TestBuildHandoffContext_NoHandoff_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := pipeline.NewHandoffStore(db)
	require.NoError(t, store.InitSchema(context.Background()))

	result := orchestrator.BuildHandoffContext(context.Background(), store, "NONEXISTENT")

	assert.Empty(t, result)
}

func TestBuildHandoffContext_WithHandoff_IncludesSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := pipeline.NewHandoffStore(db)
	require.NoError(t, store.InitSchema(ctx))

	_, err = store.Create(ctx, pipeline.Handoff{
		Ticket:    "JAM-42",
		Agent:     "engineer-1",
		Status:    "failed",
		Summary:   "Auth module broke during testing",
		Remaining: "Fix the token refresh logic",
		GitBranch: "feat/jam-42",
		GitState:  "dirty",
		Blockers:  "Flaky CI pipeline",
	})
	require.NoError(t, err)

	result := orchestrator.BuildHandoffContext(ctx, store, "JAM-42")

	assert.Contains(t, result, "## Previous Session Handoff")
	assert.Contains(t, result, "Auth module broke during testing")
	assert.Contains(t, result, "Remaining: Fix the token refresh logic")
	assert.Contains(t, result, "Blockers: Flaky CI pipeline")
	assert.Contains(t, result, "feat/jam-42")
}

func TestBuildHandoffContext_MinimalHandoff_NoOptionalFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := pipeline.NewHandoffStore(db)
	require.NoError(t, store.InitSchema(ctx))

	_, err = store.Create(ctx, pipeline.Handoff{
		Ticket:  "JAM-99",
		Agent:   "engineer-2",
		Status:  "completed",
		Summary: "All done, nothing remaining",
	})
	require.NoError(t, err)

	result := orchestrator.BuildHandoffContext(ctx, store, "JAM-99")

	assert.Contains(t, result, "All done, nothing remaining")
	assert.NotContains(t, result, "Remaining:")
	assert.NotContains(t, result, "Blockers:")
	assert.NotContains(t, result, "Branch:")
}

func TestSetHandoffStore_WiresCorrectly(t *testing.T) {
	t.Parallel()

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done — no PR"}` + "\n"),
	}
	_, handoffStore := setupHandoffOrch(t, engRunner)

	// Verify the store is functional.
	ctx := context.Background()
	_, err := handoffStore.Create(ctx, pipeline.Handoff{
		Ticket: "JAM-1", Agent: "engineer-1", Status: "completed", Summary: "test",
	})
	require.NoError(t, err)

	handoff, err := handoffStore.LatestForTicket(ctx, "JAM-1")
	require.NoError(t, err)
	assert.Equal(t, "test", handoff.Summary)
}
