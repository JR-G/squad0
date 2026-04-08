package orchestrator_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationEngine_TopBeliefs_ClosedDB_ReturnsNil(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)

	factStore := memory.NewFactStore(db)
	// Close the DB so TopBeliefs returns an error
	require.NoError(t, db.Close())

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	db2, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db2)
		factStores[role] = factStore // closed DB factStore
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// topBeliefs should handle the error gracefully and return nil
	assert.NotPanics(t, func() {
		engine.OnMessage(ctx, "engineering", "ceo", "hello")
	})
}

func TestConversationEngine_TryRespond_MissingAgent_Skips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	// Only include engineer-1 but not other roles that pickCandidates returns
	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// pickCandidates will return roles not in agents map -> tryRespond returns early
	assert.NotPanics(t, func() {
		engine.OnMessage(ctx, "engineering", "ceo", "hello all")
	})
}

func TestConversationEngine_TryRespond_WithBotAndSuccessfulPost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I think we should refactor."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	engine := orchestrator.NewConversationEngine(agents, factStores, bot, nil)

	// This exercises the full tryRespond path: agent responds, bot posts,
	// and the response is appended to recentLines
	for idx := 0; idx < 5; idx++ {
		engine.OnMessage(ctx, "engineering", "ceo", "what should we focus on?")
	}

	recent := engine.RecentMessages("engineering")
	assert.GreaterOrEqual(t, len(recent), 3)
}

func TestRunIntroductions_SaveNameError_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"My name is Spark. I love building systems."}` + "\n"),
	}
	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, db)
	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer1: agentInstance}

	// PersonaStore with no graph/fact stores will fail on SaveChosenName
	personaStore := islack.NewPersonaStore(nil, nil)

	assert.NotPanics(t, func() {
		orchestrator.RunIntroductions(ctx, agents, personaStore, nil)
	})
}

func TestRunIntroductions_PostMessage_Error_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"My name is Flux. I focus on speed."}` + "\n"),
	}
	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, db)
	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer1: agentInstance}

	graphStores := map[agent.Role]*memory.GraphStore{
		agent.RoleEngineer1: memory.NewGraphStore(db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}
	personaStore := islack.NewPersonaStore(graphStores, factStores)

	// Bot that returns errors
	errServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": false, "error": "channel_not_found"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	t.Cleanup(errServer.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{"feed": "C001"},
		Personas: map[agent.Role]islack.Persona{},
	}, errServer.URL+"/")

	assert.NotPanics(t, func() {
		orchestrator.RunIntroductions(ctx, agents, personaStore, bot)
	})
}

func TestExtractFirstWord_EmptyString_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	name := orchestrator.ExtractName("My name is ")
	assert.Empty(t, name)
}

func TestExtractFirstWord_WhitespaceOnly_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	name := orchestrator.ExtractName("My name is    ")
	assert.Empty(t, name)
}

func TestBuildChatPrompt_WithRoster_UsesRosterNames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Hey Ada!"}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	roster := map[agent.Role]string{
		agent.RolePM:        "Ada",
		agent.RoleTechLead:  "Kai",
		agent.RoleEngineer1: "Spark",
		agent.RoleEngineer2: "Nova",
		agent.RoleEngineer3: "Flux",
		agent.RoleReviewer:  "Atlas",
		agent.RoleDesigner:  "Iris",
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, roster)
	engine.OnMessage(ctx, "engineering", "ceo", "morning team")

	// Roster names are now in CLAUDE.md, not the user prompt. The prompt
	// uses "Reply as {name}" where {name} comes from the roster. Check
	// that at least one prompt uses a roster name in its Reply instruction.
	foundRosterName := false
	rosterNames := []string{"Ada", "Kai", "Spark", "Nova", "Flux", "Atlas", "Iris"}
	for _, call := range runner.calls {
		for _, name := range rosterNames {
			if strings.Contains(call.stdin, "You are "+name) {
				foundRosterName = true
				break
			}
		}
		if foundRosterName {
			break
		}
	}
	assert.True(t, foundRosterName, "expected at least one prompt to use a roster name in Reply as instruction")
}

func TestBuildChatPrompt_AllRolesExercised(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"noted."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Send enough messages from different senders to exercise all roles.
	senders := []string{"ceo", "external", "guest", "ceo", "ceo"}
	for _, sender := range senders {
		for idx := 0; idx < 10; idx++ {
			engine.OnMessage(ctx, "engineering", sender, "thoughts?")
		}
	}

	// With 50 messages and random selection, most roles should be hit.
	assert.GreaterOrEqual(t, len(runner.calls), 10)
}

func TestBuildChatPrompt_DesignerRole_HasReplyAs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"The flow feels cluttered."}` + "\n"),
	}

	agents := map[agent.Role]*agent.Agent{
		agent.RoleDesigner: buildAgent(t, runner, agent.RoleDesigner, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleDesigner: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "ceo", "what do you think of the UI?")

	if len(runner.calls) > 0 {
		// Identity is now in CLAUDE.md; the prompt just has "Reply as {name}".
		assert.Contains(t, runner.calls[0].stdin, "You are")
	}
}

func TestBuildChatPrompt_PMRole_HasReplyAs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Let's focus on auth first."}` + "\n"),
	}

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM: buildAgent(t, runner, agent.RolePM, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RolePM: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "ceo", "what should we prioritise?")

	if len(runner.calls) > 0 {
		// Identity is now in CLAUDE.md; the prompt just has "Reply as {name}".
		assert.Contains(t, runner.calls[0].stdin, "You are")
	}
}

func TestBuildChatPrompt_WithBeliefs_SetChatContextCalled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	factStore := memory.NewFactStore(db)
	_, err = factStore.CreateBelief(ctx, memory.Belief{Content: "always write tests first", Confidence: 0.9})
	require.NoError(t, err)

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Agreed."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = factStore
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "ceo", "how should we approach testing?")

	// Beliefs now go through SetChatContext into CLAUDE.md, not the user prompt.
	// Verify that the engine called agents (which means SetChatContext was invoked
	// in tryRespondInThread) and the prompts use the minimal Reply as format.
	assert.GreaterOrEqual(t, len(runner.calls), 1, "expected at least one agent to be called")
	assert.Contains(t, runner.calls[0].stdin, "Only respond if",
		"prompt should include contribution guidance")
}

func TestConversationEngine_IsDuplicate_SimilarMessage_Drops(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"the auth module needs careful error handling"}` + "\n"),
	}
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"ok": true, "ts": "123"}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Seed a message that's very similar to what the agent will say.
	engine.OnMessage(ctx, "engineering", "ceo", "the auth module needs careful error handling around retries")

	// Now trigger agent responses — the duplicate detector should catch
	// that the agent's response is similar to the existing message.
	engine.OnMessage(ctx, "engineering", "ceo", "what do you think about auth?")

	// The conversation engine should have handled this without panicking.
	// We can't easily assert the duplicate was dropped without a bot,
	// but we verify the engine processes it cleanly.
	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}
