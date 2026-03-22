package cli_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteCommand_StatusError_ReturnsErrorMessage(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)

	// Create an orchestrator with a closed DB to force an error.
	coordDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	store := coordination.NewCheckInStore(coordDB)
	require.NoError(t, store.InitSchema(context.Background()))
	_ = coordDB.Close()

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 10 * time.Second, MaxParallel: 3, CooldownAfter: 5 * time.Second},
		map[agent.Role]*agent.Agent{}, store, bot, orchestrator.NewAssigner(nil),
	)

	dispatcher := cli.NewCommandDispatcher(orch, bot)
	cmd := slack.Command{Name: "status"}

	result := dispatcher.RouteCommand(context.Background(), cmd)

	assert.Contains(t, result, "Error")
}

func TestHandlePauseResume_AllError_ReturnsError(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)

	// Create an orchestrator with a closed DB to force errors.
	coordDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	store := coordination.NewCheckInStore(coordDB)
	require.NoError(t, store.InitSchema(context.Background()))
	_ = coordDB.Close()

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM: nil,
	}
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 10 * time.Second, MaxParallel: 3, CooldownAfter: 5 * time.Second},
		agents, store, bot, orchestrator.NewAssigner(nil),
	)

	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	pauseCmd := slack.Command{Name: "pause", Args: nil}

	result := dispatcher.RouteCommand(ctx, pauseCmd)

	assert.Contains(t, result, "Error")
}

func TestRouteCommand_StopCommand_PausesAll(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, store := newTestOrchestrator(t, bot)

	ctx := context.Background()
	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	})

	dispatcher := cli.NewCommandDispatcher(orch, bot)
	cmd := slack.Command{Name: "stop", Args: nil}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "paused")
}

func TestRouteCommand_StartCommand_ResumesAll(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, store := newTestOrchestrator(t, bot)

	ctx := context.Background()
	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusIdle, FilesTouching: []string{},
	})

	dispatcher := cli.NewCommandDispatcher(orch, bot)
	cmd := slack.Command{Name: "start", Args: nil}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "resumed")
}

func TestHandleMessage_EngineeringChannel_WithConversationEngine(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	// Use NewCommandDispatcher which passes nil conversation — tests nil guard
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	msg := slack.IncomingMessage{
		Channel: "engineering",
		User:    "U001",
		Text:    "has anyone looked at the auth module?",
		IsDM:    false,
	}

	assert.NotPanics(t, func() {
		dispatcher.HandleMessage(ctx, msg)
	})
}

func TestHandleMessage_FeedChannel_DoesNotPanic(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	msg := slack.IncomingMessage{
		Channel: "feed",
		User:    "U001",
		Text:    "nice work team",
		IsDM:    false,
	}

	assert.NotPanics(t, func() {
		dispatcher.HandleMessage(ctx, msg)
	})
}

func TestHandleMessage_CommandChannel_PostsReply(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	msg := slack.IncomingMessage{
		Channel: "commands",
		Text:    "version",
		IsDM:    false,
	}

	assert.NotPanics(t, func() {
		dispatcher.HandleMessage(ctx, msg)
	})
}

func TestHandleMessage_DM_SlackError_DoesNotPanic(t *testing.T) {
	t.Parallel()

	failServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": false, "error": "not_authed"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	defer failServer.Close()

	bot := newTestBot(failServer.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	msg := slack.IncomingMessage{
		ChannelID: "D001",
		Text:      "hey PM",
		IsDM:      true,
	}

	assert.NotPanics(t, func() {
		dispatcher.HandleMessage(ctx, msg)
	})
}

func TestHandleMessage_CommandChannel_SlackError_DoesNotPanic(t *testing.T) {
	t.Parallel()

	failServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": false, "error": "channel_not_found"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	defer failServer.Close()

	bot := newTestBot(failServer.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	msg := slack.IncomingMessage{
		Channel: "commands",
		Text:    "version",
		IsDM:    false,
	}

	assert.NotPanics(t, func() {
		dispatcher.HandleMessage(ctx, msg)
	})
}

func TestHandlePauseResume_SingleError_ReturnsError(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)

	// Create an orchestrator with a closed DB to force errors.
	coordDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	store := coordination.NewCheckInStore(coordDB)
	require.NoError(t, store.InitSchema(context.Background()))
	_ = coordDB.Close()

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 10 * time.Second, MaxParallel: 3, CooldownAfter: 5 * time.Second},
		map[agent.Role]*agent.Agent{}, store, bot, orchestrator.NewAssigner(nil),
	)

	dispatcher := cli.NewCommandDispatcher(orch, bot)
	cmd := slack.Command{Name: "pause", Args: []string{"engineer-1"}}

	result := dispatcher.RouteCommand(context.Background(), cmd)

	assert.Contains(t, result, "Error")
}
