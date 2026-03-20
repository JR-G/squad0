package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecayBeliefs_ReducesOldBeliefConfidence(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	beliefID, _ := factStore.CreateBelief(ctx, memory.Belief{
		Content: "old belief", Confidence: 0.9,
	})

	_, err := db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET created_at = ? WHERE id = ?`,
		time.Now().Add(-60*24*time.Hour), beliefID,
	)
	require.NoError(t, err)

	cfg := memory.EvolutionConfig{DecayHalfLifeDays: 30.0, MinConfidence: 0.1}
	updated, err := memory.DecayBeliefs(ctx, factStore, cfg)

	require.NoError(t, err)
	assert.Greater(t, updated, 0)

	belief, _ := factStore.GetBelief(ctx, beliefID)
	assert.Less(t, belief.Confidence, 0.9)
}

func TestDecayBeliefs_RecentBelief_NoChange(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	_, _ = factStore.CreateBelief(ctx, memory.Belief{
		Content: "fresh belief", Confidence: 0.8,
	})

	cfg := memory.EvolutionConfig{DecayHalfLifeDays: 30.0, MinConfidence: 0.1}
	updated, err := memory.DecayBeliefs(ctx, factStore, cfg)

	require.NoError(t, err)
	assert.Equal(t, 0, updated)
}

func TestDecayBeliefs_FloorAtMinConfidence(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	beliefID, _ := factStore.CreateBelief(ctx, memory.Belief{
		Content: "ancient belief", Confidence: 0.5,
	})

	_, err := db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET created_at = ? WHERE id = ?`,
		time.Now().Add(-365*24*time.Hour), beliefID,
	)
	require.NoError(t, err)

	cfg := memory.EvolutionConfig{DecayHalfLifeDays: 30.0, MinConfidence: 0.15}
	_, err = memory.DecayBeliefs(ctx, factStore, cfg)

	require.NoError(t, err)

	belief, _ := factStore.GetBelief(ctx, beliefID)
	assert.GreaterOrEqual(t, belief.Confidence, 0.15)
}

func TestGeneratePersonalitySummary_IncludesTopBeliefs(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	graphStore := memory.NewGraphStore(db)
	ctx := context.Background()

	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "tests are important", Confidence: 0.9})
	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "small functions are better", Confidence: 0.8})

	summary, err := memory.GeneratePersonalitySummary(ctx, factStore, graphStore, 10)

	require.NoError(t, err)
	assert.Contains(t, summary, "tests are important")
	assert.Contains(t, summary, "small functions are better")
	assert.Contains(t, summary, "Learned Beliefs")
}

func TestGeneratePersonalitySummary_NoBeliefs_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	graphStore := memory.NewGraphStore(db)

	summary, err := memory.GeneratePersonalitySummary(context.Background(), factStore, graphStore, 10)

	require.NoError(t, err)
	assert.Empty(t, summary)
}

func TestDefaultEvolutionConfig_ReturnsDefaults(t *testing.T) {
	t.Parallel()

	cfg := memory.DefaultEvolutionConfig()

	assert.Equal(t, 30.0, cfg.DecayHalfLifeDays)
	assert.Equal(t, 0.1, cfg.MinConfidence)
}
