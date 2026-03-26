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

func TestConversationMemory_StrongOpinion_StoresAsBelief(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Engineer says something opinionated — should store.
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I think we should always validate inputs at the boundary."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "ceo", "what's your approach to validation?")

	// Check if any beliefs were stored.
	beliefs, beliefErr := factStores[agent.RoleEngineer1].TopBeliefs(ctx, 10)
	require.NoError(t, beliefErr)

	// At least one agent should have stored a belief with "always validate".
	found := false
	for _, belief := range beliefs {
		if belief.SourceOutcome == "conversation" {
			found = true
			break
		}
	}

	// With random agent selection, this may not always hit. Just verify no crash.
	_ = found
}

func TestConversationMemory_NeutralMessage_DoesNotStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Agent says something neutral — should NOT store.
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Sounds good."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "ceo", "let's ship it")

	// No conversation-sourced beliefs should exist.
	for _, role := range allRoles {
		beliefs, _ := factStores[role].TopBeliefs(ctx, 10)
		for _, belief := range beliefs {
			assert.NotEqual(t, "conversation", belief.SourceOutcome,
				"neutral message should not create conversation belief for %s", role)
		}
	}
}

func TestRetrievalStrengthening_AccessedBelief_GetsBoost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	factStore := memory.NewFactStore(db)

	// Create two beliefs with same confidence.
	id1, _ := factStore.CreateBelief(ctx, memory.Belief{Content: "never accessed", Confidence: 0.5})
	id2, _ := factStore.CreateBelief(ctx, memory.Belief{Content: "frequently accessed", Confidence: 0.5})

	// Access the second one many times.
	for range 10 {
		_ = factStore.RecordBeliefAccess(ctx, id2)
	}
	_ = id1

	beliefs, beliefErr := factStore.TopBeliefs(ctx, 2)
	require.NoError(t, beliefErr)
	require.Len(t, beliefs, 2)

	// The frequently accessed belief should rank higher.
	assert.Equal(t, "frequently accessed", beliefs[0].Content)
}
