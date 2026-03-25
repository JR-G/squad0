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

func openTestDB(t *testing.T) *memory.DB {
	t.Helper()
	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPersonaStore_LoadPersona_NoName_ReturnsFallback(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStores := map[agent.Role]*memory.GraphStore{
		agent.RolePM: memory.NewGraphStore(db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RolePM: memory.NewFactStore(db),
	}
	store := slack.NewPersonaStore(graphStores, factStores)

	persona := store.LoadPersona(context.Background(), agent.RolePM)

	assert.Equal(t, "pm", persona.Name)
	assert.Equal(t, agent.RolePM, persona.Role)
	assert.NotEmpty(t, persona.IconURL)
}

func TestPersonaStore_LoadPersona_WithChosenName_ReturnsName(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStores := map[agent.Role]*memory.GraphStore{
		agent.RoleEngineer1: memory.NewGraphStore(db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}
	store := slack.NewPersonaStore(graphStores, factStores)
	ctx := context.Background()

	err := store.SaveChosenName(ctx, agent.RoleEngineer1, "Ada")
	require.NoError(t, err)

	persona := store.LoadPersona(ctx, agent.RoleEngineer1)

	assert.Equal(t, "Ada", persona.Name)
	assert.Equal(t, agent.RoleEngineer1, persona.Role)
}

func TestPersonaStore_LoadPersona_UnknownRole_ReturnsFallback(t *testing.T) {
	t.Parallel()

	store := slack.NewPersonaStore(nil, nil)

	persona := store.LoadPersona(context.Background(), agent.RoleDesigner)

	assert.Equal(t, "designer", persona.Name)
}

func TestPersonaStore_SaveChosenName_StoresInDB(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	role := agent.RoleTechLead
	graphStores := map[agent.Role]*memory.GraphStore{role: memory.NewGraphStore(db)}
	factStores := map[agent.Role]*memory.FactStore{role: memory.NewFactStore(db)}
	store := slack.NewPersonaStore(graphStores, factStores)
	ctx := context.Background()

	err := store.SaveChosenName(ctx, role, "Rex")

	require.NoError(t, err)
	assert.True(t, store.HasChosenName(ctx, role))
}

func TestPersonaStore_HasChosenName_NoName_ReturnsFalse(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	role := agent.RoleReviewer
	graphStores := map[agent.Role]*memory.GraphStore{role: memory.NewGraphStore(db)}
	factStores := map[agent.Role]*memory.FactStore{role: memory.NewFactStore(db)}
	store := slack.NewPersonaStore(graphStores, factStores)

	assert.False(t, store.HasChosenName(context.Background(), role))
}

func TestPersonaStore_LoadAllPersonas_ReturnsAllRoles(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStores := make(map[agent.Role]*memory.GraphStore)
	factStores := make(map[agent.Role]*memory.FactStore)
	for _, role := range agent.AllRoles() {
		graphStores[role] = memory.NewGraphStore(db)
		factStores[role] = memory.NewFactStore(db)
	}
	store := slack.NewPersonaStore(graphStores, factStores)

	personas := store.LoadAllPersonas(context.Background())

	assert.Len(t, personas, len(agent.AllRoles()))
}

func TestGenerateIdenticonURL_DifferentNames_DifferentURLs(t *testing.T) {
	t.Parallel()

	urlA := slack.GenerateIdenticonURL("Ada")
	urlB := slack.GenerateIdenticonURL("Rex")

	assert.NotEqual(t, urlA, urlB)
}

func TestGenerateIdenticonURL_SameName_SameURL(t *testing.T) {
	t.Parallel()

	urlA := slack.GenerateIdenticonURL("Ada")
	urlB := slack.GenerateIdenticonURL("Ada")

	assert.Equal(t, urlA, urlB)
}

func TestGenerateIdenticonURL_ContainsHash(t *testing.T) {
	t.Parallel()

	url := slack.GenerateIdenticonURL("test")

	assert.Contains(t, url, "dicebear")
	assert.Contains(t, url, "micah")
}
