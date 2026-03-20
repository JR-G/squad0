//go:build sqlite_fts5

package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/require"
)

func TestGraphStore_FindOrCreateEntity_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	require.NoError(t, db.Close())

	_, _, err := store.FindOrCreateEntity(
		context.Background(), memory.EntityModule, "fail", "should fail",
	)

	require.Error(t, err)
}

func TestGraphStore_FindOrCreateEntity_CreateFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	_, err := db.RawDB().Exec(`DROP TABLE entities`)
	require.NoError(t, err)

	_, _, err = store.FindOrCreateEntity(
		ctx, memory.EntityModule, "fail", "should fail on create",
	)

	require.Error(t, err)
}

func TestGraphStore_GetEntity_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	require.NoError(t, db.Close())

	_, err := store.GetEntity(context.Background(), 1)

	require.Error(t, err)
}

func TestGraphStore_FindEntityByName_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	require.NoError(t, db.Close())

	_, err := store.FindEntityByName(context.Background(), memory.EntityModule, "test")

	require.Error(t, err)
}

func TestGraphStore_RelatedEntities_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	entityA, err := store.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "a",
	})
	require.NoError(t, err)

	entityB, err := store.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "b",
	})
	require.NoError(t, err)

	_, err = store.CreateRelationship(ctx, memory.Relationship{
		SourceID: entityA, TargetID: entityB,
		Type: memory.RelationDependsOn, Confidence: 0.5,
	})
	require.NoError(t, err)

	require.NoError(t, db.Close())

	_, err = store.RelatedEntities(ctx, entityA, 1)

	require.Error(t, err)
}
