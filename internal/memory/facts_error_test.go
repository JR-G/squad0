//go:build sqlite_fts5

package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactStore_CreateFact_DroppedFTS_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "fts-fail",
	})
	require.NoError(t, err)

	_, err = db.RawDB().Exec(`DROP TABLE facts_fts`)
	require.NoError(t, err)

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID:   entityID,
		Content:    "should fail on FTS insert",
		Type:       memory.FactObservation,
		Confidence: 0.5,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "FTS")
}

func TestFactStore_CreateBelief_DroppedFTS_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	_, err := db.RawDB().Exec(`DROP TABLE beliefs_fts`)
	require.NoError(t, err)

	_, err = factStore.CreateBelief(ctx, memory.Belief{
		Content:    "should fail on FTS insert",
		Confidence: 0.5,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "FTS")
}

func TestFactStore_CreateFact_DroppedTable_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	_, err := db.RawDB().Exec(`DROP TABLE facts`)
	require.NoError(t, err)

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID:   1,
		Content:    "should fail on insert",
		Type:       memory.FactObservation,
		Confidence: 0.5,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "inserting fact")
}

func TestFactStore_CreateBelief_DroppedTable_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	_, err := db.RawDB().Exec(`DROP TABLE beliefs`)
	require.NoError(t, err)

	_, err = factStore.CreateBelief(ctx, memory.Belief{
		Content:    "should fail on insert",
		Confidence: 0.5,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "inserting belief")
}

func TestFactStore_FactsByEntity_ClosedDB_ScanError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	require.NoError(t, db.Close())

	_, err := factStore.FactsByEntity(context.Background(), 1)

	assert.Error(t, err)
}

func TestFactStore_TopBeliefs_ClosedDB_ScanError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	require.NoError(t, db.Close())

	_, err := factStore.TopBeliefs(context.Background(), 10)

	assert.Error(t, err)
}
