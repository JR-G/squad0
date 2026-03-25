package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

	// Send a message to exercise the voice-injected prompts.
	engine.OnMessage(ctx, "engineering", "ceo", "what's the plan?")

	// At least one prompt should contain voice text.
	foundVoice := false
	for _, call := range runner.calls {
		if strings.Contains(call.stdin, "Distinct voice for") {
			foundVoice = true
			break
		}
	}
	assert.True(t, foundVoice, "expected prompts to include voice sections")
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

func TestFollowUpIfQuestion_AgentAsksQuestion_GetsResponse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Engineer 1 asks a question, someone should follow up.
	questionRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"What caused the first submission to fail?"}` + "\n"),
	}
	answerRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"The reviewer didn't run the gh command."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))

	agents[agent.RoleEngineer1] = buildAgent(t, questionRunner, agent.RoleEngineer1, db)
	factStores[agent.RoleEngineer1] = memory.NewFactStore(db)

	for _, role := range allRoles {
		if role == agent.RoleEngineer1 {
			continue
		}
		agents[role] = buildAgent(t, answerRunner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Human message triggers eng1 who asks a question — follow-up should fire.
	engine.OnMessage(ctx, "engineering", "ceo", "what happened with the review?")

	// The answer runner should have been called (follow-up to the question).
	answerRunner.mu.Lock()
	callCount := len(answerRunner.calls)
	answerRunner.mu.Unlock()

	assert.GreaterOrEqual(t, callCount, 1, "expected at least one follow-up to the question")
}
