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

func setupFailedReconcileOrch(t *testing.T) (orch *orchestrator.Orchestrator, store *pipeline.WorkItemStore, itemID int64, prURL string) {
	t.Helper()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store = pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	prURL = "https://github.com/test/pull/" + t.Name()
	itemID, _ = store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-FX", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, store.SetPRURL(ctx, itemID, prURL))
	require.NoError(t, store.AdvanceForce(ctx, itemID, pipeline.StageFailed, "test setup"))

	checkInDB, _ := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	checkIns := coordination.NewCheckInStore(checkInDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	orch = orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(store)

	return orch, store, itemID, prURL
}

func TestReconcileWithStates_FailedItemNotInGitHubMap_Skipped(t *testing.T) {
	t.Parallel()

	orch, store, itemID, _ := setupFailedReconcileOrch(t)

	orch.ReconcileWithStates(context.Background(), map[string]orchestrator.PRState{})

	item, err := store.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.StageFailed, item.Stage)
}

func TestReconcileWithStates_FailedItemStillOpen_NoTransition(t *testing.T) {
	t.Parallel()

	orch, store, itemID, prURL := setupFailedReconcileOrch(t)

	orch.ReconcileWithStates(context.Background(), map[string]orchestrator.PRState{
		prURL: {State: "OPEN"},
	})

	item, err := store.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.StageFailed, item.Stage)
}

// Regression for the JAM-24 case: PR was merged on GitHub while
// squad0 had given up (StageFailed). Reconciler must catch the
// silent merge and force-advance the work item to StageMerged.
func TestReconcileWithStates_FailedItemMergedOnGitHub_AdvancesToMerged(t *testing.T) {
	t.Parallel()

	orch, store, itemID, prURL := setupFailedReconcileOrch(t)

	orch.ReconcileWithStates(context.Background(), map[string]orchestrator.PRState{
		prURL: {State: "MERGED"},
	})

	item, err := store.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}
