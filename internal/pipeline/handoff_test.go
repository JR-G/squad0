package pipeline_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHandoffStore(t *testing.T) *pipeline.HandoffStore {
	t.Helper()

	db := openTestDB(t)
	store := pipeline.NewHandoffStore(db)
	require.NoError(t, store.InitSchema(context.Background()))

	return store
}

func TestHandoffStore_Create_ReturnsID(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)
	ctx := context.Background()

	handoffID, err := store.Create(ctx, pipeline.Handoff{
		Ticket:    "JAM-42",
		Agent:     "engineer-1",
		Status:    "completed",
		Summary:   "Implemented the auth module",
		Remaining: "",
		GitBranch: "feat/jam-42",
		GitState:  "clean",
	})

	require.NoError(t, err)
	assert.Greater(t, handoffID, int64(0))
}

func TestHandoffStore_LatestForTicket_ReturnsNewest(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)
	ctx := context.Background()

	// Create two handoffs for the same ticket.
	_, err := store.Create(ctx, pipeline.Handoff{
		Ticket:  "JAM-42",
		Agent:   "engineer-1",
		Status:  "failed",
		Summary: "First attempt — hit auth issues",
	})
	require.NoError(t, err)

	_, err = store.Create(ctx, pipeline.Handoff{
		Ticket:    "JAM-42",
		Agent:     "engineer-2",
		Status:    "partial",
		Summary:   "Second attempt — auth works, tests incomplete",
		Remaining: "Add integration tests for the auth flow",
		GitBranch: "feat/jam-42",
		GitState:  "dirty",
	})
	require.NoError(t, err)

	handoff, err := store.LatestForTicket(ctx, "JAM-42")

	require.NoError(t, err)
	assert.Equal(t, "engineer-2", handoff.Agent)
	assert.Equal(t, "partial", handoff.Status)
	assert.Equal(t, "Second attempt — auth works, tests incomplete", handoff.Summary)
	assert.Equal(t, "Add integration tests for the auth flow", handoff.Remaining)
	assert.Equal(t, "feat/jam-42", handoff.GitBranch)
	assert.Equal(t, "dirty", handoff.GitState)
}

func TestHandoffStore_LatestForTicket_NoHandoff_ReturnsError(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)

	_, err := store.LatestForTicket(context.Background(), "NONEXISTENT")

	require.Error(t, err)
}

func TestHandoffStore_LatestForTicket_NullableFields(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, pipeline.Handoff{
		Ticket:  "JAM-99",
		Agent:   "engineer-1",
		Status:  "completed",
		Summary: "Done with no extras",
	})
	require.NoError(t, err)

	handoff, err := store.LatestForTicket(ctx, "JAM-99")

	require.NoError(t, err)
	assert.Equal(t, "", handoff.Remaining)
	assert.Equal(t, "", handoff.GitBranch)
	assert.Equal(t, "", handoff.GitState)
	assert.Equal(t, "", handoff.Blockers)
}

func TestHandoffStore_Create_WithBlockers(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, pipeline.Handoff{
		Ticket:   "JAM-55",
		Agent:    "engineer-3",
		Status:   "failed",
		Summary:  "Blocked by CI",
		Blockers: "CI pipeline is broken — linter config missing",
	})
	require.NoError(t, err)

	handoff, err := store.LatestForTicket(ctx, "JAM-55")

	require.NoError(t, err)
	assert.Equal(t, "Blocked by CI", handoff.Summary)
	assert.Equal(t, "CI pipeline is broken — linter config missing", handoff.Blockers)
}

func TestHandoffStore_InitSchema_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	_ = db.Close()

	store := pipeline.NewHandoffStore(db)
	err := store.InitSchema(context.Background())

	require.Error(t, err)
}

func TestHandoffStore_Create_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)

	// Close the underlying DB via a new reference.
	db := openTestDB(t)
	closedStore := pipeline.NewHandoffStore(db)
	require.NoError(t, closedStore.InitSchema(context.Background()))
	_ = db.Close()

	_, err := closedStore.Create(context.Background(), pipeline.Handoff{
		Ticket: "JAM-1", Agent: "engineer-1", Status: "completed", Summary: "done",
	})

	require.Error(t, err)
	_ = store // keep the valid store reference alive
}

func TestHandoffStore_LatestForTicket_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewHandoffStore(db)
	require.NoError(t, store.InitSchema(context.Background()))

	_, err := store.Create(context.Background(), pipeline.Handoff{
		Ticket: "JAM-1", Agent: "engineer-1", Status: "completed", Summary: "done",
	})
	require.NoError(t, err)

	_ = db.Close()

	_, err = store.LatestForTicket(context.Background(), "JAM-1")

	require.Error(t, err)
}

func TestHandoffStore_MultipleTickets_IsolatedResults(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, pipeline.Handoff{
		Ticket: "JAM-1", Agent: "engineer-1", Status: "completed", Summary: "Ticket 1 done",
	})
	require.NoError(t, err)

	_, err = store.Create(ctx, pipeline.Handoff{
		Ticket: "JAM-2", Agent: "engineer-2", Status: "failed", Summary: "Ticket 2 failed",
	})
	require.NoError(t, err)

	handoff1, err := store.LatestForTicket(ctx, "JAM-1")
	require.NoError(t, err)
	assert.Equal(t, "Ticket 1 done", handoff1.Summary)

	handoff2, err := store.LatestForTicket(ctx, "JAM-2")
	require.NoError(t, err)
	assert.Equal(t, "Ticket 2 failed", handoff2.Summary)
}

func TestHandoffStore_SameDB_AsWorkItems(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	workStore := pipeline.NewWorkItemStore(db)
	require.NoError(t, workStore.InitSchema(ctx))

	handoffStore := pipeline.NewHandoffStore(db)
	require.NoError(t, handoffStore.InitSchema(ctx))

	// Both stores work on the same DB without interference.
	_, err = handoffStore.Create(ctx, pipeline.Handoff{
		Ticket: "JAM-1", Agent: "engineer-1", Status: "completed", Summary: "done",
	})
	require.NoError(t, err)

	handoff, err := handoffStore.LatestForTicket(ctx, "JAM-1")
	require.NoError(t, err)
	assert.Equal(t, "done", handoff.Summary)
}
