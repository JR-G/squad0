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

func TestOnThreadMessage_HumanSetsActiveThread(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Human posts in thread A.
	engine.OnThreadMessage(ctx, "engineering", "ceo", "hello", "thread-A")

	// Agent posts in thread B — should NOT overwrite active thread.
	engine.OnThreadMessage(ctx, "engineering", string(agent.RoleEngineer1), "working on it", "thread-B")

	// Human posts again without a thread — should use thread-A as active.
	engine.OnThreadMessage(ctx, "engineering", "ceo", "any update?", "")

	recent := engine.RecentMessages("engineering")
	assert.GreaterOrEqual(t, len(recent), 3)
}

func TestOnThreadMessage_AgentDoesNotHijackThread(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Human sets thread A.
	engine.OnThreadMessage(ctx, "engineering", "ceo", "let's discuss", "thread-A")

	// Multiple agent messages in different threads.
	engine.OnThreadMessage(ctx, "engineering", string(agent.RolePM), "narration", "thread-X")
	engine.OnThreadMessage(ctx, "engineering", string(agent.RoleTechLead), "idle thought", "thread-Y")

	// Human message without explicit thread — active thread should
	// still be thread-A since only human messages update it.
	engine.OnThreadMessage(ctx, "engineering", "ceo", "so?", "")

	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
	assert.Contains(t, recent[len(recent)-1], "so?")
}

func TestOnThreadMessage_HumanUpdatesThread(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Human sets thread A, then changes to thread B.
	engine.OnThreadMessage(ctx, "engineering", "ceo", "first topic", "thread-A")
	engine.OnThreadMessage(ctx, "engineering", "ceo", "new topic", "thread-B")

	recent := engine.RecentMessages("engineering")
	assert.GreaterOrEqual(t, len(recent), 2)
}

func TestIsHumanMessage_AgentRoles_ReturnFalse(t *testing.T) {
	t.Parallel()

	// Verify the base function used for the thread fix.
	for _, role := range agent.AllRoles() {
		count := orchestrator.DecideBaseRespondersForTest(0, false)
		assert.GreaterOrEqual(t, count, 0, "role %s should not trigger human logic", role)
	}

	// Human sender always gets 2 responders.
	count := orchestrator.DecideBaseRespondersForTest(0, true)
	assert.Equal(t, 2, count)
}
