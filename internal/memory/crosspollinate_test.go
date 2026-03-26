package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmOrCreate_NewBelief_CreatesIt(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewFactStore(db)
	ctx := context.Background()

	err := store.ConfirmOrCreate(ctx, "guard clauses improve readability", "conversation")

	require.NoError(t, err)

	beliefs, beliefErr := store.TopBeliefs(ctx, 10)
	require.NoError(t, beliefErr)
	require.Len(t, beliefs, 1)
	assert.Contains(t, beliefs[0].Content, "guard clauses")
	assert.Equal(t, 1, beliefs[0].Confirmations)
}

func TestConfirmOrCreate_ExistingBelief_ConfirmsIt(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewFactStore(db)
	ctx := context.Background()

	// Create the initial belief.
	_, err := store.CreateBelief(ctx, memory.Belief{
		Content:       "guard clauses improve readability",
		Confidence:    0.5,
		Confirmations: 1,
	})
	require.NoError(t, err)

	// ConfirmOrCreate with similar content should confirm, not duplicate.
	err = store.ConfirmOrCreate(ctx, "guard clauses readability improvement", "cross-pollination")
	require.NoError(t, err)

	beliefs, beliefErr := store.TopBeliefs(ctx, 10)
	require.NoError(t, beliefErr)

	// Should still be 1 belief (confirmed), not 2.
	// The FTS match finds the original and confirms it.
	found := false
	for _, belief := range beliefs {
		if belief.Confirmations >= 2 {
			found = true
		}
	}
	assert.True(t, found, "existing belief should have been confirmed")
}

func TestConfirmOrCreate_EmptyContent_NoError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewFactStore(db)

	err := store.ConfirmOrCreate(context.Background(), "", "test")

	require.NoError(t, err)
}

func TestSearchBeliefsByKeyword_FindsMatching(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewFactStore(db)
	ctx := context.Background()

	_, _ = store.CreateBelief(ctx, memory.Belief{
		Content:    "the auth module needs retry logic",
		Confidence: 0.7,
	})
	_, _ = store.CreateBelief(ctx, memory.Belief{
		Content:    "payments handler is well tested",
		Confidence: 0.6,
	})

	results, err := store.SearchBeliefsByKeyword(ctx, "auth retry", 5)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0], "auth module")
}

func TestSearchBeliefsByKeyword_EmptyQuery_ReturnsNil(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewFactStore(db)

	results, err := store.SearchBeliefsByKeyword(context.Background(), "", 5)

	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestSearchBeliefsByKeyword_NoMatch_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewFactStore(db)
	ctx := context.Background()

	_, _ = store.CreateBelief(ctx, memory.Belief{
		Content: "error handling is important", Confidence: 0.5,
	})

	results, err := store.SearchBeliefsByKeyword(ctx, "nonexistent", 5)

	require.NoError(t, err)
	assert.Empty(t, results)
}
