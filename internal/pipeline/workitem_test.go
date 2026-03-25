package pipeline_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func newTestStore(t *testing.T) *pipeline.WorkItemStore {
	t.Helper()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))

	return store
}

func TestStage_IsTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stage    pipeline.Stage
		terminal bool
	}{
		{pipeline.StageAssigned, false},
		{pipeline.StageWorking, false},
		{pipeline.StagePROpened, false},
		{pipeline.StageReviewing, false},
		{pipeline.StageChangesRequested, false},
		{pipeline.StageApproved, false},
		{pipeline.StageMerged, true},
		{pipeline.StageFailed, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.terminal, tt.stage.IsTerminal())
		})
	}
}

func TestWorkItemStore_Create_ReturnsID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	itemID, err := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-17",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-17",
	})

	require.NoError(t, err)
	assert.Greater(t, itemID, int64(0))
}

func TestWorkItemStore_GetByID_ReturnsItem(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	itemID, err := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-17",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-17",
	})
	require.NoError(t, err)

	item, err := store.GetByID(ctx, itemID)

	require.NoError(t, err)
	assert.Equal(t, "JAM-17", item.Ticket)
	assert.Equal(t, agent.RoleEngineer1, item.Engineer)
	assert.Equal(t, pipeline.StageWorking, item.Stage)
	assert.Equal(t, "feat/jam-17", item.Branch)
}

func TestWorkItemStore_Advance_UpdatesStage(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	require.NoError(t, store.Advance(ctx, itemID, pipeline.StagePROpened))

	item, _ := store.GetByID(ctx, itemID)
	assert.Equal(t, pipeline.StagePROpened, item.Stage)
	assert.Nil(t, item.FinishedAt)
}

func TestWorkItemStore_Advance_TerminalSetsFinishedAt(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved,
	})

	require.NoError(t, store.Advance(ctx, itemID, pipeline.StageMerged))

	item, _ := store.GetByID(ctx, itemID)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
	assert.NotNil(t, item.FinishedAt)
}

func TestWorkItemStore_SetPRURL(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	require.NoError(t, store.SetPRURL(ctx, itemID, "https://github.com/JR-G/makebook/pull/42"))

	item, _ := store.GetByID(ctx, itemID)
	assert.Equal(t, "https://github.com/JR-G/makebook/pull/42", item.PRURL)
}

func TestWorkItemStore_SetReviewer(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StagePROpened,
	})

	require.NoError(t, store.SetReviewer(ctx, itemID, agent.RoleReviewer))

	item, _ := store.GetByID(ctx, itemID)
	assert.Equal(t, agent.RoleReviewer, item.Reviewer)
}

func TestWorkItemStore_IncrementReviewCycles(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})

	require.NoError(t, store.IncrementReviewCycles(ctx, itemID))
	require.NoError(t, store.IncrementReviewCycles(ctx, itemID))

	item, _ := store.GetByID(ctx, itemID)
	assert.Equal(t, 2, item.ReviewCycles)
}

func TestWorkItemStore_OpenByEngineer_ReturnsNonTerminal(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	// Create one open and one merged item.
	openID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})

	mergedID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-10", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved,
	})
	_ = store.Advance(ctx, mergedID, pipeline.StageMerged)

	items, err := store.OpenByEngineer(ctx, agent.RoleEngineer1)

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, openID, items[0].ID)
}

func TestWorkItemStore_OpenByEngineer_Empty(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	items, err := store.OpenByEngineer(context.Background(), agent.RoleEngineer2)

	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestWorkItemStore_CompletedByEngineer_ReturnsMergedOnly(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	mergedID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-10", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved,
	})
	_ = store.Advance(ctx, mergedID, pipeline.StageMerged)

	// Still open — should not appear.
	_, _ = store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	items, err := store.CompletedByEngineer(ctx, agent.RoleEngineer1)

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "JAM-10", items[0].Ticket)
}

func TestWorkItemStore_ActiveByTicket_ReturnsLatestNonTerminal(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	// First attempt failed.
	failedID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	_ = store.Advance(ctx, failedID, pipeline.StageFailed)

	// Second attempt is active.
	activeID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer2, Stage: pipeline.StageWorking,
	})

	item, err := store.ActiveByTicket(ctx, "JAM-17")

	require.NoError(t, err)
	assert.Equal(t, activeID, item.ID)
	assert.Equal(t, agent.RoleEngineer2, item.Engineer)
}

func TestWorkItemStore_ActiveByTicket_NoResult_ReturnsError(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	_, err := store.ActiveByTicket(context.Background(), "NONEXISTENT")

	require.Error(t, err)
}

func TestWorkItemStore_FullLifecycle(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	// Create.
	itemID, err := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-42", Engineer: agent.RoleEngineer1, Stage: pipeline.StageAssigned, Branch: "feat/jam-42",
	})
	require.NoError(t, err)

	// Advance through all stages.
	stages := []pipeline.Stage{
		pipeline.StageWorking,
		pipeline.StagePROpened,
		pipeline.StageReviewing,
		pipeline.StageChangesRequested,
		pipeline.StageReviewing,
		pipeline.StageApproved,
		pipeline.StageMerged,
	}

	for _, stage := range stages {
		require.NoError(t, store.Advance(ctx, itemID, stage))
	}

	// Verify final state.
	item, err := store.GetByID(ctx, itemID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
	assert.NotNil(t, item.FinishedAt)
	assert.True(t, item.Stage.IsTerminal())

	// Should not appear in open items.
	open, _ := store.OpenByEngineer(ctx, agent.RoleEngineer1)
	assert.Empty(t, open)

	// Should appear in completed items.
	completed, _ := store.CompletedByEngineer(ctx, agent.RoleEngineer1)
	require.Len(t, completed, 1)
	assert.Equal(t, "JAM-42", completed[0].Ticket)
}

func TestWorkItemStore_GetByID_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	_, err := store.GetByID(context.Background(), 999)

	require.Error(t, err)
}

func TestWorkItemStore_ClosedDB_AllMethodsReturnErrors(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	_ = db.Close()

	ctx := context.Background()

	_, err := store.Create(ctx, pipeline.WorkItem{Ticket: "T-1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking})
	assert.Error(t, err)

	assert.Error(t, store.Advance(ctx, 1, pipeline.StageMerged))
	assert.Error(t, store.SetPRURL(ctx, 1, "url"))
	assert.Error(t, store.SetReviewer(ctx, 1, agent.RoleReviewer))
	assert.Error(t, store.IncrementReviewCycles(ctx, 1))

	_, err = store.GetByID(ctx, 1)
	assert.Error(t, err)

	_, err = store.ActiveByTicket(ctx, "T-1")
	assert.Error(t, err)

	_, err = store.OpenByEngineer(ctx, agent.RoleEngineer1)
	assert.Error(t, err)

	_, err = store.CompletedByEngineer(ctx, agent.RoleEngineer1)
	assert.Error(t, err)
}

func TestWorkItemStore_ActiveByTicket_AllTerminal_ReturnsError(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-99", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	_ = store.Advance(ctx, itemID, pipeline.StageFailed)

	// No active (non-terminal) items — should return error.
	_, err := store.ActiveByTicket(ctx, "JAM-99")
	require.Error(t, err)
}

func TestWorkItemStore_InitSchema_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	_ = db.Close()

	store := pipeline.NewWorkItemStore(db)

	err := store.InitSchema(context.Background())

	require.Error(t, err)
}
