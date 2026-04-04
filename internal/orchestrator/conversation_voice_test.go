package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetVoices_LoadsPersonalityVoiceSections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"yeah that works"}` + "\n"),
	}

	personalityDir := t.TempDir()
	for _, role := range agent.AllRoles() {
		content := "# " + string(role) + "\n\n## Voice\n\nDistinct voice for " + string(role) + ".\n\n## How You Work\n\n- Do stuff\n"
		require.NoError(t, os.WriteFile(
			filepath.Join(personalityDir, role.PersonalityFile()),
			[]byte(content), 0o644,
		))
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	loader := agent.NewPersonalityLoader(personalityDir)
	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.SetVoices(loader)

	// Send a message to exercise the engine with voices loaded.
	engine.OnMessage(ctx, "engineering", "ceo", "what's the plan?")

	// Voice is now in CLAUDE.md (per-session file), not the user prompt.
	// Verify the engine processed messages and at least one prompt uses
	// the minimal Reply as format.
	assert.GreaterOrEqual(t, len(runner.calls), 1, "expected at least one agent to be called")
	assert.Contains(t, runner.calls[0].stdin, "Reply as",
		"prompt should use the minimal Reply as format")
}

func TestSetVoices_NilLoader_DoesNotPanic(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)

	assert.NotPanics(t, func() {
		engine.SetVoices(nil)
	})
}

func TestIsQuiet_NewChannel_ReturnsTrue(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)
	assert.True(t, engine.IsQuiet("unknown-channel", 5*time.Second))
}

func TestIsQuiet_RecentMessage_ReturnsFalse(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)
	engine.OnMessage(context.Background(), "engineering", "ceo", "hello")
	assert.False(t, engine.IsQuiet("engineering", 5*time.Second))
}

func TestIsQuiet_OldMessage_ReturnsTrue(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)
	engine.SetLastMessageTime("engineering", time.Now().Add(-10*time.Second))
	assert.True(t, engine.IsQuiet("engineering", 5*time.Second))
}

func TestOnThreadMessage_Question_GetsResponse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Good question — I think we should use interfaces."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Set lastMessage to 6 minutes ago so normal decay would return 0.
	engine.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))

	// A question should still get at least one response even with stale channel.
	engine.OnMessage(ctx, "engineering", "ceo", "what do you think about caching?")

	// Human message always gets 2 responders regardless.
	assert.GreaterOrEqual(t, len(runner.calls), 1)
}

func TestOnThreadMessage_MentionedAgent_RespondsEvenWhenDecayed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	eng1Runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Yeah, I can help with that."}` + "\n"),
	}
	otherRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))

	agents[agent.RoleEngineer1] = buildAgent(t, eng1Runner, agent.RoleEngineer1, db)
	factStores[agent.RoleEngineer1] = memory.NewFactStore(db)

	for _, role := range allRoles {
		if role == agent.RoleEngineer1 {
			continue
		}
		agents[role] = buildAgent(t, otherRunner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Callum",
		agent.RoleEngineer2: "Mara",
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, roster)

	// Simulate an agent-to-agent mention after decay would normally kill responses.
	// Even with time > 5min, mentioned agents must respond.
	engine.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	engine.OnMessage(ctx, "engineering", string(agent.RoleEngineer2), "Callum, can you check the auth module?")

	assert.NotEmpty(t, eng1Runner.calls, "Callum should respond when mentioned even in decayed channel")
}

func TestDecidedThread_AgentMessagesGetNoResponders(t *testing.T) {
	t.Parallel()

	// Once a thread has a DECISION, agent messages should get 0
	// responders (only human messages or mentions break through).
	count := orchestrator.AdjustForPhaseForTest(1, orchestrator.PhaseDecided, false)
	assert.Equal(t, 0, count, "decided phase should suppress agent responses")

	humanCount := orchestrator.AdjustForPhaseForTest(1, orchestrator.PhaseDecided, true)
	assert.Equal(t, 1, humanCount, "decided phase should still respond to humans")
}
