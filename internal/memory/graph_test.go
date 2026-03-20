package memory_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphStore_CreateEntity_ReturnsID(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)

	entityID, err := store.CreateEntity(context.Background(), memory.Entity{
		Type:    memory.EntityModule,
		Name:    "payments",
		Summary: "Stripe integration",
	})

	require.NoError(t, err)
	assert.Greater(t, entityID, int64(0))
}

func TestGraphStore_GetEntity_ReturnsEntity(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	entityID, err := store.CreateEntity(ctx, memory.Entity{
		Type:    memory.EntityFile,
		Name:    "main.go",
		Summary: "entry point",
	})
	require.NoError(t, err)

	entity, err := store.GetEntity(ctx, entityID)

	require.NoError(t, err)
	assert.Equal(t, entityID, entity.ID)
	assert.Equal(t, memory.EntityFile, entity.Type)
	assert.Equal(t, "main.go", entity.Name)
	assert.Equal(t, "entry point", entity.Summary)
}

func TestGraphStore_GetEntity_NotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)

	_, err := store.GetEntity(context.Background(), 999)

	require.Error(t, err)
}

func TestGraphStore_FindEntityByName_ReturnsEntity(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	_, err := store.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule,
		Name: "auth",
	})
	require.NoError(t, err)

	entity, err := store.FindEntityByName(ctx, memory.EntityModule, "auth")

	require.NoError(t, err)
	assert.Equal(t, "auth", entity.Name)
}

func TestGraphStore_FindEntityByName_NotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)

	_, err := store.FindEntityByName(context.Background(), memory.EntityModule, "nonexistent")

	require.Error(t, err)
}

func TestGraphStore_FindOrCreateEntity_CreatesNew(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)

	entity, created, err := store.FindOrCreateEntity(
		context.Background(), memory.EntityConcept, "retry logic", "exponential backoff",
	)

	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "retry logic", entity.Name)
	assert.Greater(t, entity.ID, int64(0))
}

func TestGraphStore_FindOrCreateEntity_FindsExisting(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	first, created, err := store.FindOrCreateEntity(ctx, memory.EntityTool, "golangci-lint", "linter")
	require.NoError(t, err)
	require.True(t, created)

	second, created, err := store.FindOrCreateEntity(ctx, memory.EntityTool, "golangci-lint", "linter")

	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first.ID, second.ID)
}

func TestGraphStore_UpdateEntitySummary_UpdatesSummary(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	entityID, err := store.CreateEntity(ctx, memory.Entity{
		Type:    memory.EntityModule,
		Name:    "config",
		Summary: "old summary",
	})
	require.NoError(t, err)

	err = store.UpdateEntitySummary(ctx, entityID, "new summary")
	require.NoError(t, err)

	entity, err := store.GetEntity(ctx, entityID)

	require.NoError(t, err)
	assert.Equal(t, "new summary", entity.Summary)
}

func TestGraphStore_CreateRelationship_ReturnsID(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	sourceID, err := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "auth"})
	require.NoError(t, err)
	targetID, err := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "db"})
	require.NoError(t, err)

	relID, err := store.CreateRelationship(ctx, memory.Relationship{
		SourceID:    sourceID,
		TargetID:    targetID,
		Type:        memory.RelationDependsOn,
		Description: "auth reads from db",
		Confidence:  0.8,
	})

	require.NoError(t, err)
	assert.Greater(t, relID, int64(0))
}

func TestGraphStore_InvalidateRelationship_SetsValidUntil(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	sourceID, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "a"})
	targetID, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "b"})
	relID, _ := store.CreateRelationship(ctx, memory.Relationship{
		SourceID: sourceID, TargetID: targetID, Type: memory.RelationPairsWith, Confidence: 0.5,
	})

	err := store.InvalidateRelationship(ctx, relID)
	require.NoError(t, err)

	var validUntil sql.NullString
	err = db.RawDB().QueryRow(`SELECT valid_until FROM relationships WHERE id = ?`, relID).Scan(&validUntil)
	require.NoError(t, err)
	assert.True(t, validUntil.Valid)
}

func TestGraphStore_RelatedEntities_ReturnsNeighbours(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	centreID, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "centre"})
	neighbourID, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "neighbour"})
	_, _ = store.CreateRelationship(ctx, memory.Relationship{
		SourceID: centreID, TargetID: neighbourID, Type: memory.RelationDependsOn, Confidence: 0.7,
	})

	related, err := store.RelatedEntities(ctx, centreID, 1)

	require.NoError(t, err)
	require.Len(t, related, 1)
	assert.Equal(t, "neighbour", related[0].Name)
}

func TestGraphStore_RelatedEntities_TwoHops(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	entityA, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "a"})
	entityB, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "b"})
	entityC, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "c"})
	_, _ = store.CreateRelationship(ctx, memory.Relationship{
		SourceID: entityA, TargetID: entityB, Type: memory.RelationDependsOn, Confidence: 0.5,
	})
	_, _ = store.CreateRelationship(ctx, memory.Relationship{
		SourceID: entityB, TargetID: entityC, Type: memory.RelationDependsOn, Confidence: 0.5,
	})

	related, err := store.RelatedEntities(ctx, entityA, 2)

	require.NoError(t, err)
	assert.Len(t, related, 2)
}

func TestGraphStore_RelatedEntities_ExcludesInvalidated(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	entityA, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "a"})
	entityB, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "b"})
	relID, _ := store.CreateRelationship(ctx, memory.Relationship{
		SourceID: entityA, TargetID: entityB, Type: memory.RelationDependsOn, Confidence: 0.5,
	})
	_ = store.InvalidateRelationship(ctx, relID)

	related, err := store.RelatedEntities(ctx, entityA, 1)

	require.NoError(t, err)
	assert.Empty(t, related)
}

func TestGraphStore_RelatedEntities_NoRelationships_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewGraphStore(db)
	ctx := context.Background()

	entityID, _ := store.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "isolated"})

	related, err := store.RelatedEntities(ctx, entityID, 2)

	require.NoError(t, err)
	assert.Empty(t, related)
}
