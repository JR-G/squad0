package slack_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersonaStore_LoadPersona_NoFactStore_ReturnsFallback(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStores := map[agent.Role]*memory.GraphStore{
		agent.RolePM: memory.NewGraphStore(db),
	}
	store := slack.NewPersonaStore(graphStores, nil)

	persona := store.LoadPersona(context.Background(), agent.RolePM)

	assert.Equal(t, "pm", persona.Name)
	assert.Equal(t, agent.RolePM, persona.Role)
}

func TestPersonaStore_LoadPersona_NoGraphStore_ReturnsFallback(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStores := map[agent.Role]*memory.FactStore{
		agent.RolePM: memory.NewFactStore(db),
	}
	store := slack.NewPersonaStore(nil, factStores)

	persona := store.LoadPersona(context.Background(), agent.RolePM)

	assert.Equal(t, "pm", persona.Name)
}

func TestPersonaStore_SaveChosenName_NoGraphStore_ReturnsError(t *testing.T) {
	t.Parallel()

	store := slack.NewPersonaStore(nil, nil)

	err := store.SaveChosenName(context.Background(), agent.RolePM, "Nova")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no graph store")
}

func TestPersonaStore_SaveChosenName_NoFactStore_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStores := map[agent.Role]*memory.GraphStore{
		agent.RolePM: memory.NewGraphStore(db),
	}
	store := slack.NewPersonaStore(graphStores, nil)

	err := store.SaveChosenName(context.Background(), agent.RolePM, "Nova")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no fact store")
}

func TestPersonaStore_SaveChosenName_ClosedDB_FindOrCreateError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStores := map[agent.Role]*memory.GraphStore{
		agent.RolePM: memory.NewGraphStore(db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RolePM: memory.NewFactStore(db),
	}
	store := slack.NewPersonaStore(graphStores, factStores)

	require.NoError(t, db.Close())

	err := store.SaveChosenName(context.Background(), agent.RolePM, "Nova")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating identity entity")
}

func TestPersonaStore_SaveChosenName_ClosedDB_CreateFactError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	role := agent.RoleEngineer1
	graphStores := map[agent.Role]*memory.GraphStore{role: memory.NewGraphStore(db)}
	factStores := map[agent.Role]*memory.FactStore{role: memory.NewFactStore(db)}
	store := slack.NewPersonaStore(graphStores, factStores)
	ctx := context.Background()

	err := store.SaveChosenName(ctx, role, "FirstName")
	require.NoError(t, err)

	_, err = db.RawDB().Exec(`DROP TABLE facts`)
	require.NoError(t, err)

	err = store.SaveChosenName(ctx, role, "SecondName")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing name")
}

func TestPersonaStore_LoadPersona_NonPreferenceFact_IgnoredByExtract(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	role := agent.RoleEngineer2
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	graphStores := map[agent.Role]*memory.GraphStore{role: graphStore}
	factStores := map[agent.Role]*memory.FactStore{role: factStore}
	store := slack.NewPersonaStore(graphStores, factStores)
	ctx := context.Background()

	entity, _, err := graphStore.FindOrCreateEntity(ctx, memory.EntityConcept, "identity", "test identity")
	require.NoError(t, err)

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID:      entity.ID,
		Content:       "some observation about identity",
		Type:          memory.FactObservation,
		Confidence:    0.8,
		Confirmations: 1,
	})
	require.NoError(t, err)

	persona := store.LoadPersona(ctx, role)

	assert.Equal(t, string(role), persona.Name)
}

func TestPersonaStore_LoadPersona_ShortContent_IgnoredByParseName(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	role := agent.RoleEngineer3
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	graphStores := map[agent.Role]*memory.GraphStore{role: graphStore}
	factStores := map[agent.Role]*memory.FactStore{role: factStore}
	store := slack.NewPersonaStore(graphStores, factStores)
	ctx := context.Background()

	entity, _, err := graphStore.FindOrCreateEntity(ctx, memory.EntityConcept, "identity", "test identity")
	require.NoError(t, err)

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID:      entity.ID,
		Content:       "short",
		Type:          memory.FactPreference,
		Confidence:    1.0,
		Confirmations: 100,
	})
	require.NoError(t, err)

	persona := store.LoadPersona(ctx, role)

	assert.Equal(t, string(role), persona.Name)
}

func TestPersonaStore_LoadPersona_WrongPrefix_IgnoredByParseName(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	role := agent.RoleReviewer
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	graphStores := map[agent.Role]*memory.GraphStore{role: graphStore}
	factStores := map[agent.Role]*memory.FactStore{role: factStore}
	store := slack.NewPersonaStore(graphStores, factStores)
	ctx := context.Background()

	entity, _, err := graphStore.FindOrCreateEntity(ctx, memory.EntityConcept, "identity", "test identity")
	require.NoError(t, err)

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID:      entity.ID,
		Content:       "my preferred name is Something",
		Type:          memory.FactPreference,
		Confidence:    1.0,
		Confirmations: 100,
	})
	require.NoError(t, err)

	persona := store.LoadPersona(ctx, role)

	assert.Equal(t, string(role), persona.Name)
}

func TestPersonaStore_LoadPersona_FactsByEntityError_ReturnsFallback(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	role := agent.RoleTechLead
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	graphStores := map[agent.Role]*memory.GraphStore{role: graphStore}
	factStores := map[agent.Role]*memory.FactStore{role: factStore}
	store := slack.NewPersonaStore(graphStores, factStores)
	ctx := context.Background()

	_, _, err := graphStore.FindOrCreateEntity(ctx, memory.EntityConcept, "identity", "test identity")
	require.NoError(t, err)

	_, err = db.RawDB().Exec(`DROP TABLE facts`)
	require.NoError(t, err)

	persona := store.LoadPersona(ctx, role)

	assert.Equal(t, string(role), persona.Name)
}
