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

func TestSeedHistory_PopulatesRecentMessages(t *testing.T) {
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

	// Seed with messages from before a restart.
	engine.SeedHistory("engineering", []string{
		"ceo: what's the status on auth?",
		"engineer-1: Almost done, just need to fix the retry logic.",
		"engineer-2: I can help with that after my current ticket.",
	})

	// Verify the messages are in the recent history.
	recent := engine.RecentMessages("engineering")
	assert.Len(t, recent, 3)
	assert.Contains(t, recent[0], "status on auth")
	assert.Contains(t, recent[1], "retry logic")
	assert.Contains(t, recent[2], "help with that")
}

func TestSeedHistory_DoesNotTriggerResponses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I have thoughts on this."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	runner.mu.Lock()
	beforeCount := len(runner.calls)
	runner.mu.Unlock()

	// Seed should NOT trigger agent responses.
	engine.SeedHistory("engineering", []string{
		"ceo: what should we do about the auth module?",
	})

	runner.mu.Lock()
	afterCount := len(runner.calls)
	runner.mu.Unlock()

	assert.Equal(t, beforeCount, afterCount, "SeedHistory should not trigger agent responses")
}

func TestSeedHistory_EmptyMessages_NoError(t *testing.T) {
	t.Parallel()

	engine := orchestrator.NewConversationEngine(nil, nil, nil, nil)

	assert.NotPanics(t, func() {
		engine.SeedHistory("engineering", nil)
	})

	recent := engine.RecentMessages("engineering")
	assert.Empty(t, recent)
}

func TestSeedHistory_MultipleChannels_Independent(t *testing.T) {
	t.Parallel()

	engine := orchestrator.NewConversationEngine(nil, nil, nil, nil)

	engine.SeedHistory("engineering", []string{"eng msg 1", "eng msg 2"})
	engine.SeedHistory("reviews", []string{"review msg 1"})

	engRecent := engine.RecentMessages("engineering")
	reviewRecent := engine.RecentMessages("reviews")

	assert.Len(t, engRecent, 2)
	assert.Len(t, reviewRecent, 1)
}

func TestSeedHistory_RespectsMaxRecent(t *testing.T) {
	t.Parallel()

	engine := orchestrator.NewConversationEngine(nil, nil, nil, nil)

	// Seed more than the maxRecent (15).
	messages := make([]string, 20)
	for idx := range messages {
		messages[idx] = "message"
	}

	engine.SeedHistory("engineering", messages)

	recent := engine.RecentMessages("engineering")
	assert.LessOrEqual(t, len(recent), 15, "should respect maxRecent limit")
}
