//go:build sqlite_fts5

package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecayBeliefs_WithLastConfirmedAt_UsesConfirmedDate(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	beliefID, err := factStore.CreateBelief(ctx, memory.Belief{
		Content: "confirmed belief", Confidence: 0.9,
	})
	require.NoError(t, err)

	// Set created_at to ancient, but last_confirmed_at to just minutes ago
	recentTime := time.Now().Add(-10 * time.Minute)
	_, err = db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET created_at = ?, last_confirmed_at = ? WHERE id = ?`,
		time.Now().Add(-365*24*time.Hour), recentTime, beliefID,
	)
	require.NoError(t, err)

	cfg := memory.EvolutionConfig{DecayHalfLifeDays: 30.0, MinConfidence: 0.1}
	updated, err := memory.DecayBeliefs(ctx, factStore, cfg)

	require.NoError(t, err)
	// With very recent last_confirmed_at, decay should be negligible (< 0.01 diff)
	assert.Equal(t, 0, updated)

	belief, err := factStore.GetBelief(ctx, beliefID)
	require.NoError(t, err)
	// Should stay at 0.9 since decay is negligible with 10-minute age
	assert.InDelta(t, 0.9, belief.Confidence, 0.01)
}

func TestDecayBeliefs_WithOldLastConfirmedAt_Decays(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	beliefID, err := factStore.CreateBelief(ctx, memory.Belief{
		Content: "old confirmed belief", Confidence: 0.8,
	})
	require.NoError(t, err)

	// Set both created_at and last_confirmed_at to 60 days ago
	oldTime := time.Now().Add(-60 * 24 * time.Hour)
	_, err = db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET created_at = ?, last_confirmed_at = ? WHERE id = ?`,
		oldTime, oldTime, beliefID,
	)
	require.NoError(t, err)

	cfg := memory.DefaultEvolutionConfig()
	updated, err := memory.DecayBeliefs(ctx, factStore, cfg)

	require.NoError(t, err)
	assert.Equal(t, 1, updated)

	belief, err := factStore.GetBelief(ctx, beliefID)
	require.NoError(t, err)
	assert.Less(t, belief.Confidence, 0.5)
}

func TestDecayBeliefs_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	// Create an old belief so decay would trigger
	beliefID, err := factStore.CreateBelief(ctx, memory.Belief{
		Content: "doomed belief", Confidence: 0.8,
	})
	require.NoError(t, err)

	_, err = db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET created_at = ? WHERE id = ?`,
		time.Now().Add(-90*24*time.Hour), beliefID,
	)
	require.NoError(t, err)

	// Cancel the context before calling DecayBeliefs
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	cfg := memory.EvolutionConfig{DecayHalfLifeDays: 30.0, MinConfidence: 0.1}
	_, err = memory.DecayBeliefs(cancelCtx, factStore, cfg)

	// Either TopBeliefs or updateBeliefConfidence will fail due to cancelled context
	require.Error(t, err)
}

func TestDecayBeliefs_NoBeliefs_ReturnsZero(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)

	cfg := memory.EvolutionConfig{DecayHalfLifeDays: 30.0, MinConfidence: 0.1}
	updated, err := memory.DecayBeliefs(context.Background(), factStore, cfg)

	require.NoError(t, err)
	assert.Equal(t, 0, updated)
}

func TestDecayBeliefs_MultipleBeliefs_DecaysAll(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	ids := make([]int64, 3)
	for idx := range ids {
		contents := []string{"belief alpha", "belief beta", "belief gamma"}
		id, err := factStore.CreateBelief(ctx, memory.Belief{
			Content: contents[idx], Confidence: 0.9,
		})
		require.NoError(t, err)
		ids[idx] = id
	}

	// Age all beliefs — set both created_at and last_confirmed_at to past
	oldTime := time.Now().Add(-60 * 24 * time.Hour)
	for _, id := range ids {
		_, err := db.RawDB().ExecContext(ctx,
			`UPDATE beliefs SET created_at = ?, last_confirmed_at = ? WHERE id = ?`,
			oldTime, oldTime, id,
		)
		require.NoError(t, err)
	}

	cfg := memory.DefaultEvolutionConfig()
	updated, err := memory.DecayBeliefs(ctx, factStore, cfg)

	require.NoError(t, err)
	assert.Equal(t, 3, updated)

	for _, id := range ids {
		belief, err := factStore.GetBelief(ctx, id)
		require.NoError(t, err)
		assert.Less(t, belief.Confidence, 0.9)
	}
}

func TestDecayBeliefs_TopBeliefsError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	require.NoError(t, db.Close())

	cfg := memory.EvolutionConfig{DecayHalfLifeDays: 30.0, MinConfidence: 0.1}
	_, err := memory.DecayBeliefs(context.Background(), factStore, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading beliefs for decay")
}
