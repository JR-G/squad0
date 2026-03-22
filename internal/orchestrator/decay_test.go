package orchestrator_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationEngine_AgentMessages_IncreaseRoundCount(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	passRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}

	agents := make(map[agent.Role]*agent.Agent, len(agent.AllRoles()))
	factStores := make(map[agent.Role]*memory.FactStore, len(agent.AllRoles()))
	for _, role := range agent.AllRoles() {
		agents[role] = buildAgent(t, passRunner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil)

	// Simulate agent-only conversation (no human reset)
	for range 15 {
		engine.OnMessage(ctx, "engineering", string(agent.RoleEngineer1), "thinking...")
	}

	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}

func TestConversationEngine_HumanMessage_ResetsDecay(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Good point."}` + "\n")}

	agents := make(map[agent.Role]*agent.Agent, len(agent.AllRoles()))
	factStores := make(map[agent.Role]*memory.FactStore, len(agent.AllRoles()))
	for _, role := range agent.AllRoles() {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil)

	// Agent messages build up rounds
	for range 10 {
		engine.OnMessage(ctx, "engineering", string(agent.RolePM), "update")
	}

	// Human message resets
	engine.OnMessage(ctx, "engineering", "james", "what's everyone working on?")

	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}
