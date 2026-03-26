package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactStore_CreateFact_ReturnsID(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "payments"})

	factID, err := factStore.CreateFact(ctx, memory.Fact{
		EntityID:   entityID,
		Content:    "Stripe webhooks need timeout handling",
		Type:       memory.FactWarning,
		Confidence: 0.5,
	})

	require.NoError(t, err)
	assert.Greater(t, factID, int64(0))
}

func TestFactStore_GetFact_ReturnsFact(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "auth"})
	factID, _ := factStore.CreateFact(ctx, memory.Fact{
		EntityID:   entityID,
		Content:    "JWT tokens expire after 1 hour",
		Type:       memory.FactObservation,
		Confidence: 0.6,
	})

	fact, err := factStore.GetFact(ctx, factID)

	require.NoError(t, err)
	assert.Equal(t, "JWT tokens expire after 1 hour", fact.Content)
	assert.Equal(t, memory.FactObservation, fact.Type)
	assert.Equal(t, entityID, fact.EntityID)
}

func TestFactStore_FactsByEntity_ReturnsOrderedByConfidence(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "api"})
	_, _ = factStore.CreateFact(ctx, memory.Fact{EntityID: entityID, Content: "low confidence", Type: memory.FactObservation, Confidence: 0.3})
	_, _ = factStore.CreateFact(ctx, memory.Fact{EntityID: entityID, Content: "high confidence", Type: memory.FactObservation, Confidence: 0.9})

	facts, err := factStore.FactsByEntity(ctx, entityID)

	require.NoError(t, err)
	require.Len(t, facts, 2)
	assert.Equal(t, "high confidence", facts[0].Content)
	assert.Equal(t, "low confidence", facts[1].Content)
}

func TestFactStore_ConfirmFact_IncreasesConfidence(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "db"})
	factID, _ := factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "WAL mode is fast", Type: memory.FactObservation,
		Confidence: 0.5, Confirmations: 1,
	})

	err := factStore.ConfirmFact(ctx, factID)
	require.NoError(t, err)

	fact, err := factStore.GetFact(ctx, factID)
	require.NoError(t, err)
	assert.Equal(t, 2, fact.Confirmations)
	assert.Greater(t, fact.Confidence, 0.5)
}

func TestFactStore_InvalidateFact_SetsTimestamp(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "old"})
	factID, _ := factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "outdated fact", Type: memory.FactObservation, Confidence: 0.5,
	})

	err := factStore.InvalidateFact(ctx, factID)
	require.NoError(t, err)

	fact, err := factStore.GetFact(ctx, factID)
	require.NoError(t, err)
	assert.NotNil(t, fact.InvalidatedAt)
}

func TestFactStore_FactsByEntity_ExcludesInvalidated(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "test"})
	factID, _ := factStore.CreateFact(ctx, memory.Fact{EntityID: entityID, Content: "valid", Type: memory.FactObservation, Confidence: 0.5})
	invalidID, _ := factStore.CreateFact(ctx, memory.Fact{EntityID: entityID, Content: "invalid", Type: memory.FactObservation, Confidence: 0.5})
	_ = factStore.InvalidateFact(ctx, invalidID)
	_ = factID

	facts, err := factStore.FactsByEntity(ctx, entityID)

	require.NoError(t, err)
	require.Len(t, facts, 1)
	assert.Equal(t, "valid", facts[0].Content)
}

func TestFactStore_CreateBelief_ReturnsID(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)

	beliefID, err := factStore.CreateBelief(context.Background(), memory.Belief{
		Content:    "small functions are better",
		Confidence: 0.5,
	})

	require.NoError(t, err)
	assert.Greater(t, beliefID, int64(0))
}

func TestFactStore_GetBelief_ReturnsBelief(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	beliefID, _ := factStore.CreateBelief(ctx, memory.Belief{
		Content:    "tests prevent regressions",
		Confidence: 0.7,
	})

	belief, err := factStore.GetBelief(ctx, beliefID)

	require.NoError(t, err)
	assert.Equal(t, "tests prevent regressions", belief.Content)
	assert.InDelta(t, 0.7, belief.Confidence, 0.01)
}

func TestFactStore_ConfirmBelief_IncreasesConfidence(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	beliefID, _ := factStore.CreateBelief(ctx, memory.Belief{
		Content: "code review catches bugs", Confidence: 0.5, Confirmations: 1,
	})

	err := factStore.ConfirmBelief(ctx, beliefID)
	require.NoError(t, err)

	belief, err := factStore.GetBelief(ctx, beliefID)
	require.NoError(t, err)
	assert.Equal(t, 2, belief.Confirmations)
	assert.Greater(t, belief.Confidence, 0.5)
	assert.NotNil(t, belief.LastConfirmedAt)
}

func TestFactStore_ContradictBelief_DecreasesConfidence(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	beliefID, _ := factStore.CreateBelief(ctx, memory.Belief{
		Content: "mocks are sufficient", Confidence: 0.5, Confirmations: 1,
	})

	err := factStore.ContradictBelief(ctx, beliefID)
	require.NoError(t, err)

	belief, err := factStore.GetBelief(ctx, beliefID)
	require.NoError(t, err)
	assert.Equal(t, 1, belief.Contradictions)
	assert.Less(t, belief.Confidence, 0.5)
}

func TestFactStore_TopBeliefs_ReturnsOrderedByConfidence(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "low", Confidence: 0.2})
	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "high", Confidence: 0.9})
	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "mid", Confidence: 0.5})

	beliefs, err := factStore.TopBeliefs(ctx, 2)

	require.NoError(t, err)
	require.Len(t, beliefs, 2)
	assert.Equal(t, "high", beliefs[0].Content)
	assert.Equal(t, "mid", beliefs[1].Content)
}

func TestFactStore_TopBeliefs_TemporalDecay_OldBeliefRanksLower(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	// Create a high-confidence belief and age it 90 days.
	oldID, _ := factStore.CreateBelief(ctx, memory.Belief{Content: "old wisdom", Confidence: 0.9})
	_, _ = db.RawDB().ExecContext(ctx,
		"UPDATE beliefs SET created_at = datetime('now', '-90 days') WHERE id = ?", oldID)

	// Create a lower-confidence recent belief.
	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "fresh insight", Confidence: 0.5})

	beliefs, err := factStore.TopBeliefs(ctx, 2)

	require.NoError(t, err)
	require.Len(t, beliefs, 2)
	// The fresh insight should rank higher because 90-day decay reduces
	// 0.9 to ~0.9 * 0.125 = 0.11, which is below 0.5.
	assert.Equal(t, "fresh insight", beliefs[0].Content)
	assert.Equal(t, "old wisdom", beliefs[1].Content)
}

func TestFactStore_TopBeliefs_RecentlyConfirmed_ResistsDecay(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	// Create a belief that was created 90 days ago but confirmed yesterday.
	oldButConfirmedID, _ := factStore.CreateBelief(ctx, memory.Belief{Content: "confirmed old", Confidence: 0.9})
	_, _ = db.RawDB().ExecContext(ctx,
		"UPDATE beliefs SET created_at = datetime('now', '-90 days'), last_confirmed_at = datetime('now', '-1 day') WHERE id = ?",
		oldButConfirmedID)

	// Create a new belief with lower confidence.
	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "new but weak", Confidence: 0.4})

	beliefs, err := factStore.TopBeliefs(ctx, 2)

	require.NoError(t, err)
	require.Len(t, beliefs, 2)
	// The confirmed old belief decays from last_confirmed_at (1 day ago),
	// not created_at (90 days ago). So it stays near 0.9 and beats 0.4.
	assert.Equal(t, "confirmed old", beliefs[0].Content)
}
