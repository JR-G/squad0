package pipeline_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// RecentFailures — previously at 0% coverage
// ---------------------------------------------------------------------------

func TestRecentFailures_ReturnsRecentlyFailedItems(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	ctx := context.Background()

	// Insert a failed item with an updated_at written via Go time.Time
	// binding so that the comparison format matches what RecentFailures
	// uses (Go time.Time → driver serialisation).
	recentTime := time.Now().Add(-5 * time.Minute)
	_, err := db.ExecContext(ctx, `
		INSERT INTO work_items (ticket, engineer, stage, updated_at)
		VALUES (?, ?, 'failed', ?)`,
		"JAM-FAIL-1", string(agent.RoleEngineer1), recentTime)
	require.NoError(t, err)

	failures := store.RecentFailures(ctx, 1*time.Hour)

	require.Len(t, failures, 1)
	assert.Equal(t, "JAM-FAIL-1", failures[0].Ticket)
	assert.Equal(t, pipeline.StageFailed, failures[0].Stage)
}

func TestRecentFailures_ExcludesOldFailures(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))

	ctx := context.Background()

	// Insert a failed item with an old timestamp via Go time binding.
	oldTime := time.Now().Add(-2 * time.Hour)
	_, err := db.ExecContext(ctx, `
		INSERT INTO work_items (ticket, engineer, stage, updated_at)
		VALUES (?, ?, 'failed', ?)`,
		"JAM-OLD-FAIL", string(agent.RoleEngineer1), oldTime)
	require.NoError(t, err)

	// Window of 30 minutes should exclude the 2-hour-old failure.
	failures := store.RecentFailures(ctx, 30*time.Minute)

	assert.Empty(t, failures)
}

func TestRecentFailures_ExcludesNonFailedItems(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	ctx := context.Background()

	// Insert a working item with a recent timestamp via Go time binding.
	_, err := db.ExecContext(ctx, `
		INSERT INTO work_items (ticket, engineer, stage, updated_at)
		VALUES (?, ?, 'working', ?)`,
		"JAM-WORKING", string(agent.RoleEngineer1), time.Now())
	require.NoError(t, err)

	failures := store.RecentFailures(ctx, 1*time.Hour)

	assert.Empty(t, failures)
}

func TestRecentFailures_EmptyTable(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	failures := store.RecentFailures(context.Background(), 1*time.Hour)

	assert.Empty(t, failures)
}

func TestRecentFailures_ClosedDB_ReturnsNil(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	_ = db.Close()

	failures := store.RecentFailures(context.Background(), 1*time.Hour)

	assert.Nil(t, failures)
}

func TestRecentFailures_MultipleFailures_ReturnsMostRecentFirst(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	ctx := context.Background()

	// Insert two failed items with different timestamps via Go time binding.
	olderTime := time.Now().Add(-10 * time.Minute)
	newerTime := time.Now().Add(-5 * time.Minute)

	_, err := db.ExecContext(ctx, `
		INSERT INTO work_items (ticket, engineer, stage, updated_at)
		VALUES (?, ?, 'failed', ?)`,
		"JAM-FIRST", string(agent.RoleEngineer1), olderTime)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO work_items (ticket, engineer, stage, updated_at)
		VALUES (?, ?, 'failed', ?)`,
		"JAM-SECOND", string(agent.RoleEngineer2), newerTime)
	require.NoError(t, err)

	failures := store.RecentFailures(ctx, 1*time.Hour)

	require.Len(t, failures, 2)
	// Most recent first (ORDER BY updated_at DESC).
	assert.Equal(t, "JAM-SECOND", failures[0].Ticket)
	assert.Equal(t, "JAM-FIRST", failures[1].Ticket)
}

// ---------------------------------------------------------------------------
// InitSchema — error paths for index creation
// ---------------------------------------------------------------------------

func TestInitSchema_IndexCreationFails_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)

	// First init succeeds.
	require.NoError(t, store.InitSchema(context.Background()))

	// Close the DB, then try to init again — all three statements
	// (table + two indexes) fail. The table creation error is hit.
	_ = db.Close()
	err := store.InitSchema(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating work_items table")
}

// ---------------------------------------------------------------------------
// OpenWithPR — error path from closed DB
// ---------------------------------------------------------------------------

func TestOpenWithPR_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	_ = db.Close()

	_, err := store.OpenWithPR(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "querying open items with PR")
}

// ---------------------------------------------------------------------------
// GetByTicket — error path from closed DB
// ---------------------------------------------------------------------------

func TestGetByTicket_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	_ = db.Close()

	_, err := store.GetByTicket(context.Background(), "JAM-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "querying items for ticket")
}

// ---------------------------------------------------------------------------
// CompletedTickets — error path from closed DB
// ---------------------------------------------------------------------------

func TestCompletedTickets_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	_ = db.Close()

	_, err := store.CompletedTickets(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "querying completed tickets")
}

// ---------------------------------------------------------------------------
// CompletedTickets — scan error path
// ---------------------------------------------------------------------------

func TestCompletedTickets_DuplicateMergedTicket_ReturnsDistinct(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))

	ctx := context.Background()

	// Create two merged items for the same ticket.
	id1, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-DUP", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved,
	})
	require.NoError(t, store.Advance(ctx, id1, pipeline.StageMerged))

	id2, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-DUP", Engineer: agent.RoleEngineer2, Stage: pipeline.StageApproved,
	})
	require.NoError(t, store.Advance(ctx, id2, pipeline.StageMerged))

	tickets, err := store.CompletedTickets(ctx)
	require.NoError(t, err)

	// DISTINCT should give us only one entry.
	assert.Len(t, tickets, 1)
	assert.Equal(t, "JAM-DUP", tickets[0])
}

// ---------------------------------------------------------------------------
// OpenWithPR — mixed terminal and non-terminal items with PRs
// ---------------------------------------------------------------------------

func TestOpenWithPR_FailedItemWithPR_Excluded(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	// Create an item, set a PR, then fail it.
	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-FAILPR", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, store.SetPRURL(ctx, itemID, "https://github.com/test/pull/99"))
	require.NoError(t, store.Advance(ctx, itemID, pipeline.StageFailed))

	items, err := store.OpenWithPR(ctx)
	require.NoError(t, err)
	assert.Empty(t, items, "failed items with PR should be excluded from OpenWithPR")
}

func TestFailedWithPR_OnlyFailedItemsWithPR(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	// Failed with PR: should be returned.
	failedID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-FW1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, store.SetPRURL(ctx, failedID, "https://github.com/test/pull/1"))
	require.NoError(t, store.Advance(ctx, failedID, pipeline.StageFailed))

	// Open with PR: should NOT be returned.
	openID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-FW2", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, store.SetPRURL(ctx, openID, "https://github.com/test/pull/2"))

	// Failed without PR: should NOT be returned.
	noPRID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-FW3", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, store.Advance(ctx, noPRID, pipeline.StageFailed))

	items, err := store.FailedWithPR(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "JAM-FW1", items[0].Ticket)
}

func TestAdvanceForce_BypassesValidator(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-AF1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	// working → merged is illegal via Advance, legal via AdvanceForce.
	err := store.AdvanceForce(ctx, itemID, pipeline.StageMerged, "test setup fast-forward")
	require.NoError(t, err)

	item, err := store.GetByID(ctx, itemID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}

func TestAdvanceForce_EmptyReason_ReturnsError(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-AF2", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})

	err := store.AdvanceForce(ctx, itemID, pipeline.StageMerged, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason")
}

func TestFailedWithPR_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	_ = db.Close()

	_, err := store.FailedWithPR(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "querying failed items")
}

// ---------------------------------------------------------------------------
// InitSchema — idempotent when called twice
// ---------------------------------------------------------------------------

func TestInitSchema_CalledTwice_NoError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)

	require.NoError(t, store.InitSchema(context.Background()))
	require.NoError(t, store.InitSchema(context.Background()))
}

// ---------------------------------------------------------------------------
// RecentFailures — mixed failed and merged items within window
// ---------------------------------------------------------------------------

func TestRecentFailures_MixedStages_OnlyFailed(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	ctx := context.Background()

	recentTime := time.Now().Add(-5 * time.Minute)

	// Insert a merged item — should not appear in failures.
	_, err := db.ExecContext(ctx, `
		INSERT INTO work_items (ticket, engineer, stage, updated_at)
		VALUES (?, ?, 'merged', ?)`,
		"JAM-MERGED", string(agent.RoleEngineer1), recentTime)
	require.NoError(t, err)

	// Insert a failed item — should appear in failures.
	_, err = db.ExecContext(ctx, `
		INSERT INTO work_items (ticket, engineer, stage, updated_at)
		VALUES (?, ?, 'failed', ?)`,
		"JAM-FAIL", string(agent.RoleEngineer2), recentTime)
	require.NoError(t, err)

	failures := store.RecentFailures(ctx, 1*time.Hour)

	require.Len(t, failures, 1)
	assert.Equal(t, "JAM-FAIL", failures[0].Ticket)
}

// ---------------------------------------------------------------------------
// InitSchema — verify index creation error path with broken DB
// ---------------------------------------------------------------------------

func TestInitSchema_PartialFailure_ReturnsFirstError(t *testing.T) {
	t.Parallel()

	// Use a read-only in-memory database to trigger write failures.
	db, err := sql.Open("sqlite3", "file::memory:?mode=ro")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := pipeline.NewWorkItemStore(db)
	initErr := store.InitSchema(context.Background())

	// The read-only database should fail on table creation.
	require.Error(t, initErr)
}
