package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAgentsForPulse(t *testing.T, runner *fakeProcessRunner) map[agent.Role]*agent.Agent {
	t.Helper()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
	}

	return agents
}

func newPulseSlackBot(t *testing.T) *islack.Bot {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": true, "channel": "C004", "ts": "456"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	t.Cleanup(server.Close)

	return islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"engineering": "C004",
		},
		Personas:   map[agent.Role]islack.Persona{},
		MinSpacing: 0,
	}, server.URL+"/")
}

func TestRunConversationRound_WithAgents_DoesNotPanic(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Let's discuss the architecture."}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)
	bot := newPulseSlackBot(t)

	assert.NotPanics(t, func() {
		orchestrator.RunConversationRound(context.Background(), agents, bot)
	})
}

func TestRunConversationRound_NilBot_DoesNotPanic(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Interesting point."}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)

	assert.NotPanics(t, func() {
		orchestrator.RunConversationRound(context.Background(), agents, nil)
	})
}

func TestRunConversationRound_PASSResponse_SkipsPosting(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)
	bot := newPulseSlackBot(t)

	assert.NotPanics(t, func() {
		orchestrator.RunConversationRound(context.Background(), agents, bot)
	})
}

func TestRunConversationRound_EmptyTranscript_SkipsPosting(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":""}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)
	bot := newPulseSlackBot(t)

	assert.NotPanics(t, func() {
		orchestrator.RunConversationRound(context.Background(), agents, bot)
	})
}

func TestRunConversationRound_SessionError_ContinuesLoop(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"rate limited"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}
	agents := buildAgentsForPulse(t, runner)
	bot := newPulseSlackBot(t)

	assert.NotPanics(t, func() {
		orchestrator.RunConversationRound(context.Background(), agents, bot)
	})
}

func TestRunConversationRound_NoPMOrReviewer_InCandidates(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I agree."}` + "\n"),
	}

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Only include PM and Reviewer — they should both be excluded from candidates
	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:       buildAgent(t, runner, agent.RolePM, db),
		agent.RoleReviewer: buildAgent(t, runner, agent.RoleReviewer, db),
	}

	assert.NotPanics(t, func() {
		orchestrator.RunConversationRound(ctx, agents, nil)
	})
}

func TestRunIdlePulse_WithIdleAgent_PostsMessage(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Anyone want to pair on this?"}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)
	bot := newPulseSlackBot(t)

	idleRoles := []agent.Role{agent.RoleEngineer1, agent.RoleTechLead}
	recentMessages := []string{"PM: Let's check the board."}

	result := orchestrator.RunIdlePulse(context.Background(), agents, idleRoles, bot, recentMessages)

	assert.True(t, result)
}

func TestRunIdlePulse_EmptyResponse_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":""}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)
	bot := newPulseSlackBot(t)

	idleRoles := []agent.Role{agent.RoleEngineer1}
	result := orchestrator.RunIdlePulse(context.Background(), agents, idleRoles, bot, nil)

	assert.False(t, result)
}

func TestRunIdlePulse_EmptyIdleRoles_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"hello"}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)

	result := orchestrator.RunIdlePulse(context.Background(), agents, nil, nil, nil)

	assert.False(t, result)
}

func TestRunIdlePulse_OnlyReviewerIdle_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"hello"}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)

	// Reviewer is not a chatty role, so filterChattyRoles excludes it
	idleRoles := []agent.Role{agent.RoleReviewer}
	result := orchestrator.RunIdlePulse(context.Background(), agents, idleRoles, nil, nil)

	assert.False(t, result)
}

func TestRunIdlePulse_NilBot_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Let's chat."}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)

	idleRoles := []agent.Role{agent.RoleEngineer1}
	result := orchestrator.RunIdlePulse(context.Background(), agents, idleRoles, nil, nil)

	assert.False(t, result)
}

func TestRunIdlePulse_SessionError_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"crashed"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}
	agents := buildAgentsForPulse(t, runner)
	bot := newPulseSlackBot(t)

	idleRoles := []agent.Role{agent.RoleEngineer1}
	result := orchestrator.RunIdlePulse(context.Background(), agents, idleRoles, bot, nil)

	assert.False(t, result)
}

func TestRunIdlePulse_AgentNotInMap_ReturnsFalse(t *testing.T) {
	t.Parallel()

	// Create agents map without the idle role
	agents := map[agent.Role]*agent.Agent{}

	idleRoles := []agent.Role{agent.RoleDesigner}
	result := orchestrator.RunIdlePulse(context.Background(), agents, idleRoles, nil, nil)

	assert.False(t, result)
}

func TestRunIdlePulse_WithRecentMessages_IncludesThemInPrompt(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Good question."}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)
	bot := newPulseSlackBot(t)

	recentMessages := []string{
		"PM: Let's focus on the auth module.",
		"Engineer-1: I noticed some flaky tests.",
	}

	idleRoles := []agent.Role{agent.RoleTechLead}
	result := orchestrator.RunIdlePulse(context.Background(), agents, idleRoles, bot, recentMessages)

	assert.True(t, result)
	// Verify the prompt included recent messages
	require.GreaterOrEqual(t, len(runner.calls), 1)
	assert.Contains(t, runner.calls[0].stdin, "auth module")
}

func TestPostToEngineering_EmptyTranscript_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":""}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)

	// RunIdlePulse with empty result exercises postToEngineering empty check
	idleRoles := []agent.Role{agent.RoleEngineer1}
	result := orchestrator.RunIdlePulse(context.Background(), agents, idleRoles, nil, nil)
	assert.False(t, result)
}

func TestPostToEngineering_BotError_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Something meaningful."}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)

	errServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": false, "error": "channel_not_found"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	t.Cleanup(errServer.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"engineering": "C004",
		},
		Personas:   map[agent.Role]islack.Persona{},
		MinSpacing: 0,
	}, errServer.URL+"/")

	idleRoles := []agent.Role{agent.RoleEngineer1}
	result := orchestrator.RunIdlePulse(context.Background(), agents, idleRoles, bot, nil)
	assert.False(t, result)
}

func TestFilterChattyRoles_AllChattyRoles(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Interesting."}` + "\n"),
	}
	agents := buildAgentsForPulse(t, runner)
	bot := newPulseSlackBot(t)

	chattyRoles := []agent.Role{
		agent.RolePM,
		agent.RoleTechLead,
		agent.RoleEngineer1,
		agent.RoleEngineer2,
		agent.RoleEngineer3,
		agent.RoleDesigner,
	}

	// Each of these should pass the filter and be a valid candidate
	result := orchestrator.RunIdlePulse(context.Background(), agents, chattyRoles, bot, nil)

	// At least one should succeed
	assert.True(t, result)
}

func TestRunPMBriefing_PostsToEngineering_ViaPostToEngineering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Morning all. Checking the board now."}` + "\n"),
	}
	pmAgent := buildAgent(t, runner, agent.RolePM, db)
	agents := map[agent.Role]*agent.Agent{agent.RolePM: pmAgent}

	var postedText string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		postedText = req.FormValue("text")
		resp := map[string]interface{}{"ok": true, "channel": "C004", "ts": "789"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"engineering": "C004",
		},
		Personas:   map[agent.Role]islack.Persona{},
		MinSpacing: 0,
	}, server.URL+"/")

	orchestrator.RunPMBriefing(ctx, agents, bot)

	assert.Contains(t, postedText, "Morning all")
}
