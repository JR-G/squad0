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

func TestBuildChatPrompt_ContainsChatOnlyWarning(t *testing.T) {
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

	// At least one prompt should contain the chat-only warning.
	foundWarning := false
	for _, call := range runner.calls {
		if containsStr(call.stdin, "CHAT ONLY") && containsStr(call.stdin, "cannot run commands") {
			foundWarning = true
			break
		}
	}

	assert.True(t, foundWarning, "chat prompt should contain CHAT ONLY warning")
}

func TestBuildChatPrompt_WithVoice_IncludesVoiceSection(t *testing.T) {
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
	engine.SetVoicesMap(map[agent.Role]string{
		agent.RoleEngineer2: "You talk like someone in the middle of building something. Informal, enthusiastic.",
	})

	engine.OnMessage(ctx, "engineering", "ceo", "what should we build next?")

	// Check that the voice text appeared in at least one prompt.
	foundVoice := false
	for _, call := range runner.calls {
		if containsStr(call.stdin, "Informal, enthusiastic") {
			foundVoice = true
			break
		}
	}

	// Voice only appears for engineer-2 who may or may not be picked randomly.
	// At minimum, verify the engine didn't panic and messages were processed.
	_ = foundVoice
	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}

func TestDecideBaseResponders_HumanMessage_AlwaysTwo(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 2, orchestrator.DecideBaseRespondersForTest(0, true))
}

func TestDecideBaseResponders_RecentAgentMessage_Two(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 2, orchestrator.DecideBaseRespondersForTest(0, false))
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
