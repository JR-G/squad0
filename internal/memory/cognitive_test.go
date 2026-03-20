package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetrievalScore_HighConfidence_RecentAccess_HighScore(t *testing.T) {
	t.Parallel()

	score := memory.RetrievalScore(0.9, 0, 5, 0.023)
	assert.Greater(t, score, 1.0)
}

func TestRetrievalScore_LowConfidence_OldAccess_LowScore(t *testing.T) {
	t.Parallel()

	score := memory.RetrievalScore(0.3, 60, 0, 0.023)
	assert.Less(t, score, 0.3)
}

func TestRetrievalScore_ZeroDays_NoDecay(t *testing.T) {
	t.Parallel()

	score := memory.RetrievalScore(0.5, 0, 0, 0.023)
	assert.Greater(t, score, 0.0)
}

func TestRecordFactAccess_IncrementsCount(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "test"})
	factID, _ := factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "test fact", Type: memory.FactObservation, Confidence: 0.5,
	})

	err := factStore.RecordFactAccess(ctx, factID)
	require.NoError(t, err)

	fact, err := factStore.GetFact(ctx, factID)
	require.NoError(t, err)
	assert.Equal(t, 1, fact.AccessCount)
	assert.NotNil(t, fact.LastAccessedAt)
}

func TestRecordFactAccess_MultipleAccesses(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "test"})
	factID, _ := factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "accessed fact", Type: memory.FactObservation, Confidence: 0.5,
	})

	for i := 0; i < 5; i++ {
		require.NoError(t, factStore.RecordFactAccess(ctx, factID))
	}

	fact, _ := factStore.GetFact(ctx, factID)
	assert.Equal(t, 5, fact.AccessCount)
}

func TestRecordBeliefAccess_IncrementsCount(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	beliefID, _ := factStore.CreateBelief(ctx, memory.Belief{
		Content: "test belief", Confidence: 0.5,
	})

	err := factStore.RecordBeliefAccess(ctx, beliefID)
	require.NoError(t, err)

	belief, err := factStore.GetBelief(ctx, beliefID)
	require.NoError(t, err)
	assert.Equal(t, 1, belief.AccessCount)
	assert.NotNil(t, belief.LastAccessedAt)
}

func TestRecordFactAccess_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	_ = db.Close()

	err := factStore.RecordFactAccess(context.Background(), 1)
	assert.Error(t, err)
}

func TestRecordBeliefAccess_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	_ = db.Close()

	err := factStore.RecordBeliefAccess(context.Background(), 1)
	assert.Error(t, err)
}

func TestEmotionalSalienceMultiplier_Failure_Returns1_4(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 1.4, memory.EmotionalSalienceMultiplier("failure"), 0.01)
}

func TestEmotionalSalienceMultiplier_Partial_Returns1_2(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 1.2, memory.EmotionalSalienceMultiplier("partial"), 0.01)
}

func TestEmotionalSalienceMultiplier_Success_Returns1_0(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 1.0, memory.EmotionalSalienceMultiplier("success"), 0.01)
}

func TestEmotionalSalienceMultiplier_Empty_Returns1_0(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 1.0, memory.EmotionalSalienceMultiplier(""), 0.01)
}

func TestDescribeStrength_Strong(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	graphStore := memory.NewGraphStore(db)
	ctx := context.Background()

	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "strong belief", Confidence: 0.9})

	summary, err := memory.GeneratePersonalitySummary(ctx, factStore, graphStore, 10)
	require.NoError(t, err)
	assert.Contains(t, summary, "(strong)")
}

func TestDescribeStrength_Moderate(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	graphStore := memory.NewGraphStore(db)
	ctx := context.Background()

	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "moderate belief", Confidence: 0.6})

	summary, err := memory.GeneratePersonalitySummary(ctx, factStore, graphStore, 10)
	require.NoError(t, err)
	assert.Contains(t, summary, "(moderate)")
}

func TestDescribeStrength_Weak(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	graphStore := memory.NewGraphStore(db)
	ctx := context.Background()

	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "weak belief", Confidence: 0.3})

	summary, err := memory.GeneratePersonalitySummary(ctx, factStore, graphStore, 10)
	require.NoError(t, err)
	assert.Contains(t, summary, "(weak)")
}

func TestMostRecentActivity_UsesLastAccessed(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	beliefID, _ := factStore.CreateBelief(ctx, memory.Belief{
		Content: "accessed recently", Confidence: 0.8,
	})

	// Age the creation
	oldTime := time.Now().Add(-90 * 24 * time.Hour)
	_, _ = db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET created_at = ?, last_confirmed_at = ? WHERE id = ?`,
		oldTime, oldTime, beliefID,
	)

	// Access it now
	require.NoError(t, factStore.RecordBeliefAccess(ctx, beliefID))

	// Should not decay much because last_accessed_at is recent
	cfg := memory.DefaultEvolutionConfig()
	updated, err := memory.DecayBeliefs(ctx, factStore, cfg)
	require.NoError(t, err)
	assert.Equal(t, 0, updated)
}

func TestCognitiveMigration_AddsNewColumns(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ctx := context.Background()

	// Verify the new columns exist by inserting and reading
	factStore := memory.NewFactStore(db)
	graphStore := memory.NewGraphStore(db)

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "test"})
	factID, _ := factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "has new columns", Type: memory.FactObservation, Confidence: 0.5,
	})

	require.NoError(t, factStore.RecordFactAccess(ctx, factID))

	fact, err := factStore.GetFact(ctx, factID)
	require.NoError(t, err)
	assert.Equal(t, 1, fact.AccessCount)

	beliefID, _ := factStore.CreateBelief(ctx, memory.Belief{Content: "has columns too", Confidence: 0.5})
	require.NoError(t, factStore.RecordBeliefAccess(ctx, beliefID))

	belief, err := factStore.GetBelief(ctx, beliefID)
	require.NoError(t, err)
	assert.Equal(t, 1, belief.AccessCount)
}
