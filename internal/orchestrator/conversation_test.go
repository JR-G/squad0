package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConversationEngine(t *testing.T) *orchestrator.ConversationEngine {
	t.Helper()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Looks good to me."}` + "\n")}

	agents := make(map[agent.Role]*agent.Agent, 3)
	factStores := make(map[agent.Role]*memory.FactStore, 3)

	for _, role := range []agent.Role{agent.RolePM, agent.RoleEngineer1, agent.RoleTechLead} {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	return orchestrator.NewConversationEngine(agents, factStores, nil, nil)
}

func TestConversationEngine_OnMessage_TracksRecentMessages(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)

	engine.OnMessage(context.Background(), "engineering", "user1", "hello team")

	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
	assert.Contains(t, recent[0], "hello team")
}

func TestConversationEngine_OnMessage_DecaysResponders(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)
	ctx := context.Background()

	for idx := 0; idx < 10; idx++ {
		engine.OnMessage(ctx, "engineering", "user1", "message")
	}

	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}

func TestConversationEngine_ResetRound_ResetsCounter(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)
	ctx := context.Background()

	engine.OnMessage(ctx, "engineering", "user1", "first")
	engine.ResetRound("engineering")

	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}

func TestConversationEngine_PerChannelState(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)
	ctx := context.Background()

	engine.OnMessage(ctx, "engineering", "user1", "eng message")
	engine.OnMessage(ctx, "feed", "user2", "feed message")

	engRecent := engine.RecentMessages("engineering")
	feedRecent := engine.RecentMessages("feed")

	assert.NotEmpty(t, engRecent)
	assert.NotEmpty(t, feedRecent)
}

func TestConversationEngine_NilBot_DoesNotPanic(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)

	assert.NotPanics(t, func() {
		engine.OnMessage(context.Background(), "engineering", "user1", "test")
	})
}

func TestConversationEngine_BreakSilence_RecentActivity_Skips(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)

	assert.NotPanics(t, func() {
		engine.BreakSilence(context.Background())
	})
}

func TestConversationEngine_BreakSilence_QuietChannel_TriggersResponse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Just thinking out loud."}` + "\n")}

	agents := make(map[agent.Role]*agent.Agent, len(agent.AllRoles()))
	factStores := make(map[agent.Role]*memory.FactStore, len(agent.AllRoles()))
	for _, role := range agent.AllRoles() {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.SetLastMessageTime("engineering", time.Now().Add(-15*time.Minute))

	for range 20 {
		engine.BreakSilence(ctx)
	}
}

func TestConversationEngine_TryRespond_PostsToBot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Looks great."}` + "\n")}

	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "ceo", "what do you think?")

	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}

func TestConversationEngine_PASSResponse_NotPosted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}

	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "someone", "any thoughts?")

	recent := engine.RecentMessages("engineering")
	for _, line := range recent {
		assert.NotContains(t, line, "PASS")
	}
}
