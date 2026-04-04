package routing_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/routing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOpinionStore(t *testing.T) *routing.OpinionStore {
	t.Helper()
	ctx := context.Background()

	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleReviewer: memory.NewFactStore(db),
	}

	return routing.NewOpinionStore(factStores)
}

func TestOpinionStore_RecordReviewOutcome_CleanPR(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newOpinionStore(t)

	err := store.RecordReviewOutcome(ctx, agent.RoleReviewer, agent.RoleEngineer1, true, 0)
	assert.NoError(t, err)
}

func TestOpinionStore_RecordReviewOutcome_NeedsRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newOpinionStore(t)

	err := store.RecordReviewOutcome(ctx, agent.RoleReviewer, agent.RoleEngineer2, false, 2)
	assert.NoError(t, err)
}

func TestOpinionStore_RecordReviewOutcome_MissingReviewer_NoError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newOpinionStore(t)

	// Tech Lead is not in the fact stores — should not error.
	err := store.RecordReviewOutcome(ctx, agent.RoleTechLead, agent.RoleEngineer1, true, 0)
	assert.NoError(t, err)
}

func TestOpinionStore_ScrutinyFor_Default_IsNormal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newOpinionStore(t)

	level := store.ScrutinyFor(ctx, agent.RoleEngineer1)
	assert.Equal(t, routing.ScrutinyNormal, level)
}

func TestScrutinyHint_Low(t *testing.T) {
	t.Parallel()

	hint := routing.ScrutinyHint(routing.ScrutinyLow, "Mara")
	assert.Contains(t, hint, "Mara")
	assert.Contains(t, hint, "clean PRs")
}

func TestScrutinyHint_High(t *testing.T) {
	t.Parallel()

	hint := routing.ScrutinyHint(routing.ScrutinyHigh, "Callum")
	assert.Contains(t, hint, "Callum")
	assert.Contains(t, hint, "extra care")
}

func TestOpinionStore_ScrutinyFor_AfterCleanPRs_IsLow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newOpinionStore(t)

	// Record several clean PR outcomes — should shift to low scrutiny.
	for range 5 {
		_ = store.RecordReviewOutcome(ctx, agent.RoleReviewer, agent.RoleEngineer1, true, 0)
	}

	level := store.ScrutinyFor(ctx, agent.RoleEngineer1)
	assert.Equal(t, routing.ScrutinyLow, level)
}

func TestOpinionStore_ScrutinyFor_AfterRevisions_IsHigh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newOpinionStore(t)

	// Record several revision outcomes — should shift to high scrutiny.
	for range 4 {
		_ = store.RecordReviewOutcome(ctx, agent.RoleReviewer, agent.RoleEngineer2, false, 3)
	}

	level := store.ScrutinyFor(ctx, agent.RoleEngineer2)
	assert.Equal(t, routing.ScrutinyHigh, level)
}

func TestOpinionStore_RecordReviewOutcome_StandardOutcome(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newOpinionStore(t)

	// 1 fix cycle, approved — standard outcome (not clean, not bad).
	err := store.RecordReviewOutcome(ctx, agent.RoleReviewer, agent.RoleEngineer3, true, 1)
	assert.NoError(t, err)
}

func TestScrutinyHint_Normal_Empty(t *testing.T) {
	t.Parallel()

	hint := routing.ScrutinyHint(routing.ScrutinyNormal, "Anyone")
	assert.Empty(t, hint)
}
