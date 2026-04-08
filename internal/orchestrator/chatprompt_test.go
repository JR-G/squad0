package orchestrator_test

import (
	"context"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainsQuestion_WithQuestionMark_ReturnsTrue(t *testing.T) {
	t.Parallel()
	assert.True(t, orchestrator.ContainsQuestionForTest("What do you think?"))
}

func TestContainsQuestion_WithoutQuestionMark_ReturnsFalse(t *testing.T) {
	t.Parallel()
	assert.False(t, orchestrator.ContainsQuestionForTest("I think we should use goroutines"))
}

func TestBuildChatPrompt_ContainsReplyInstruction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"sounds good"}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "ceo", "can someone merge that PR?")

	// At least one prompt should contain the "You are" instruction.
	foundReplyAs := false
	for _, call := range runner.calls {
		if containsStr(call.stdin, "You are") {
			foundReplyAs = true
			break
		}
	}

	assert.True(t, foundReplyAs, "chat prompt should contain Reply as instruction")
}

func TestBuildChatPrompt_WithVoice_EngineDoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"yeah let's just try it"}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Set voices manually via SetVoicesMap (we test the real loader in personality_test.go).
	// Voice text now goes into CLAUDE.md, not the user prompt.
	engine.SetVoicesMap(map[agent.Role]string{
		agent.RoleEngineer2: "You talk like someone in the middle of building something. Informal, enthusiastic.",
	})

	engine.OnMessage(ctx, "engineering", "ceo", "what should we build next?")

	// Voice is now in CLAUDE.md (per-session file), not the user prompt.
	// Verify the engine processed messages without panicking.
	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}

func TestDecideBaseResponders_HumanMessage_AlwaysTwo(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 2, orchestrator.DecideBaseRespondersForTest(0, true))
}

func TestDecideBaseResponders_RecentAgentMessage_One(t *testing.T) {
	t.Parallel()
	// Agent messages get 1 responder — dialogues, not pile-ons.
	assert.Equal(t, 1, orchestrator.DecideBaseRespondersForTest(0, false))
}

func TestDecideBaseResponders_OlderAgentMessage_One(t *testing.T) {
	t.Parallel()
	// 3 minutes = between 2-5 min threshold.
	assert.Equal(t, 1, orchestrator.DecideBaseRespondersForTest(3*60*1e9, false))
}

func TestDecideBaseResponders_StaleMessage_Zero(t *testing.T) {
	t.Parallel()
	// 6 minutes = past 5 min threshold.
	assert.Equal(t, 0, orchestrator.DecideBaseRespondersForTest(6*60*1e9, false))
}

func containsStr(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
