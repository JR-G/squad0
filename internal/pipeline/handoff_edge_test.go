package pipeline_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandoffStore_AllForTicket_ReturnsAll(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, pipeline.Handoff{
		Ticket: "TASK-42", Agent: "engineer-1", Status: "failed", Summary: "first attempt",
	})
	require.NoError(t, err)

	_, err = store.Create(ctx, pipeline.Handoff{
		Ticket: "TASK-42", Agent: "engineer-2", Status: "completed", Summary: "second attempt",
	})
	require.NoError(t, err)

	_, err = store.Create(ctx, pipeline.Handoff{
		Ticket: "TASK-99", Agent: "engineer-1", Status: "completed", Summary: "other ticket",
	})
	require.NoError(t, err)

	handoffs, allErr := store.AllForTicket(ctx, "TASK-42")

	require.NoError(t, allErr)
	assert.Len(t, handoffs, 2)
	// Most recent first.
	assert.Equal(t, "second attempt", handoffs[0].Summary)
	assert.Equal(t, "first attempt", handoffs[1].Summary)
}

func TestHandoffStore_AllForTicket_NoHandoffs_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)

	handoffs, err := store.AllForTicket(context.Background(), "NONEXISTENT")

	require.NoError(t, err)
	assert.Empty(t, handoffs)
}

func TestHandoffStore_AllForTicket_WithBlockers_IncludesBlockers(t *testing.T) {
	t.Parallel()

	store := newHandoffStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, pipeline.Handoff{
		Ticket:   "TASK-55",
		Agent:    "engineer-3",
		Status:   "blocked",
		Summary:  "Stuck on migration",
		Blockers: "Migration fails on nullable columns",
	})
	require.NoError(t, err)

	handoffs, allErr := store.AllForTicket(ctx, "TASK-55")

	require.NoError(t, allErr)
	require.Len(t, handoffs, 1)
	assert.Equal(t, "Migration fails on nullable columns", handoffs[0].Blockers)
}
