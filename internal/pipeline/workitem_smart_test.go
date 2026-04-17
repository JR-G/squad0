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

func TestGetByTicket_ReturnsAllItems(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(ctx))

	_, _ = store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	id2, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageFailed,
	})
	_, _ = store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-2", Engineer: agent.RoleEngineer2, Stage: pipeline.StageWorking,
	})

	items, err := store.GetByTicket(ctx, "JAM-1")
	require.NoError(t, err)
	assert.Len(t, items, 2)

	_ = id2
}

func TestGetByTicket_NoResults(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(ctx))

	items, err := store.GetByTicket(ctx, "JAM-MISSING")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestCompletedTickets_ReturnsDistinctMerged(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(ctx))

	id1, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved,
	})
	require.NoError(t, store.Advance(ctx, id1, pipeline.StageMerged))

	id2, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-2", Engineer: agent.RoleEngineer2, Stage: pipeline.StageWorking,
	})
	require.NoError(t, store.Advance(ctx, id2, pipeline.StageFailed))

	id3, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-3", Engineer: agent.RoleEngineer3, Stage: pipeline.StageApproved,
	})
	require.NoError(t, store.Advance(ctx, id3, pipeline.StageMerged))

	tickets, err := store.CompletedTickets(ctx)
	require.NoError(t, err)
	assert.Len(t, tickets, 2)
	assert.Contains(t, tickets, "JAM-1")
	assert.Contains(t, tickets, "JAM-3")
	assert.NotContains(t, tickets, "JAM-2")
}

func TestCompletedTickets_Empty(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(ctx))

	tickets, err := store.CompletedTickets(ctx)
	require.NoError(t, err)
	assert.Empty(t, tickets)
}
