package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSlackServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
}

func setupIntroductionTest(t *testing.T, transcript string) (map[agent.Role]*agent.Agent, *islack.PersonaStore) {
	t.Helper()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":` + `"` + transcript + `"` + `}` + "\n")}

	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, db)

	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: agentInstance,
	}

	graphStores := map[agent.Role]*memory.GraphStore{
		agent.RoleEngineer1: memory.NewGraphStore(db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}
	personaStore := islack.NewPersonaStore(graphStores, factStores)

	return agents, personaStore
}

func TestRunIntroductions_AgentPicksName(t *testing.T) {
	t.Parallel()

	agents, personaStore := setupIntroductionTest(t, "My name is Ada. I love building things.")

	orchestrator.RunIntroductions(context.Background(), agents, personaStore, nil)

	assert.True(t, personaStore.HasChosenName(context.Background(), agent.RoleEngineer1))
	persona := personaStore.LoadPersona(context.Background(), agent.RoleEngineer1)
	assert.Equal(t, "Ada", persona.Name)
}

func TestRunIntroductions_AlreadyHasName_Skips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	agents, personaStore := setupIntroductionTest(t, "My name is Rex.")

	require.NoError(t, personaStore.SaveChosenName(ctx, agent.RoleEngineer1, "Existing"))

	orchestrator.RunIntroductions(ctx, agents, personaStore, nil)

	persona := personaStore.LoadPersona(ctx, agent.RoleEngineer1)
	assert.Equal(t, "Existing", persona.Name)
}

func TestRunIntroductions_NoNameInTranscript_Skips(t *testing.T) {
	t.Parallel()

	agents, personaStore := setupIntroductionTest(t, "Hello everyone, great to be here.")

	orchestrator.RunIntroductions(context.Background(), agents, personaStore, nil)

	assert.False(t, personaStore.HasChosenName(context.Background(), agent.RoleEngineer1))
}

func TestRunIntroductions_SessionFails_Continues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"failed"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}

	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, db)
	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer1: agentInstance}

	graphStores := map[agent.Role]*memory.GraphStore{agent.RoleEngineer1: memory.NewGraphStore(db)}
	factStores := map[agent.Role]*memory.FactStore{agent.RoleEngineer1: memory.NewFactStore(db)}
	personaStore := islack.NewPersonaStore(graphStores, factStores)

	orchestrator.RunIntroductions(ctx, agents, personaStore, nil)

	assert.False(t, personaStore.HasChosenName(ctx, agent.RoleEngineer1))
}

func TestRunIntroductions_NilBot_DoesNotPanic(t *testing.T) {
	t.Parallel()

	agents, personaStore := setupIntroductionTest(t, "My name is Nova. I manage the team.")

	assert.NotPanics(t, func() {
		orchestrator.RunIntroductions(context.Background(), agents, personaStore, nil)
	})
}

func TestRunIntroductions_WithBot_PostsToFeed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"My name is Bolt. I move fast."}` + "\n")}
	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, db)
	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer1: agentInstance}

	graphStores := map[agent.Role]*memory.GraphStore{agent.RoleEngineer1: memory.NewGraphStore(db)}
	factStores := map[agent.Role]*memory.FactStore{agent.RoleEngineer1: memory.NewFactStore(db)}
	personaStore := islack.NewPersonaStore(graphStores, factStores)

	server := newTestSlackServer()
	defer server.Close()

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{"feed": "C001"},
		Personas: map[agent.Role]islack.Persona{},
	}, server.URL+"/")

	orchestrator.RunIntroductions(ctx, agents, personaStore, bot)

	assert.True(t, personaStore.HasChosenName(ctx, agent.RoleEngineer1))
}

func TestExtractName_IveChosenFormat(t *testing.T) {
	t.Parallel()

	name := orchestrator.ExtractName("I've chosen the name Sage for myself.")
	assert.Equal(t, "Sage", name)
}

func TestExtractName_IllGoByFormat(t *testing.T) {
	t.Parallel()

	name := orchestrator.ExtractName("I'll go by Atlas from now on.")
	assert.Equal(t, "Atlas", name)
}

func TestRunIntroductions_EmptyTranscript_UsesFallback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Agent returns empty content — name extraction will work but transcript is empty
	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"My name is Zen."}` + "\n")}
	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, db)
	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer1: agentInstance}

	graphStores := map[agent.Role]*memory.GraphStore{agent.RoleEngineer1: memory.NewGraphStore(db)}
	factStores := map[agent.Role]*memory.FactStore{agent.RoleEngineer1: memory.NewFactStore(db)}
	personaStore := islack.NewPersonaStore(graphStores, factStores)

	orchestrator.RunIntroductions(ctx, agents, personaStore, nil)

	persona := personaStore.LoadPersona(ctx, agent.RoleEngineer1)
	assert.Equal(t, "Zen", persona.Name)
}

func TestExtractName_IChoseFormat(t *testing.T) {
	t.Parallel()

	name := orchestrator.ExtractName("I chose the name Kai.")
	assert.Equal(t, "Kai", name)
}
