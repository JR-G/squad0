package cli_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSlackServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"channel":"C123","ts":"1234.5678"}`))
	}))
}

func newTestBot(apiURL string) *slack.Bot {
	channels := map[string]string{
		"commands":    "C001",
		"feed":        "C002",
		"engineering": "C003",
		"reviews":     "C004",
		"triage":      "C005",
		"standup":     "C006",
	}
	personas := map[agent.Role]slack.Persona{
		agent.RolePM: {Role: agent.RolePM, Name: "PM"},
	}
	return slack.NewBotWithURL(slack.BotConfig{
		BotToken:   "xoxb-test",
		AppToken:   "xapp-test",
		Channels:   channels,
		Personas:   personas,
		MinSpacing: 0,
	}, apiURL+"/")
}

func newTestCheckInStore(t *testing.T) (*coordination.CheckInStore, *sql.DB) {
	t.Helper()

	coordDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)

	store := coordination.NewCheckInStore(coordDB)
	err = store.InitSchema(context.Background())
	require.NoError(t, err)

	return store, coordDB
}

func newTestOrchestrator(t *testing.T, bot *slack.Bot) (*orchestrator.Orchestrator, *coordination.CheckInStore) {
	t.Helper()

	store, coordDB := newTestCheckInStore(t)
	t.Cleanup(func() { _ = coordDB.Close() })

	agents := map[agent.Role]*agent.Agent{}
	assigner := orchestrator.NewAssigner(nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:  10 * time.Second,
			MaxParallel:   3,
			CooldownAfter: 5 * time.Second,
		},
		agents, store, bot, assigner,
	)

	return orch, store
}

func TestHandleMessage_NonCommandChannel_Ignored(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	msg := slack.IncomingMessage{
		Channel: "engineering",
		Text:    "status",
		IsDM:    false,
	}

	assert.NotPanics(t, func() {
		dispatcher.HandleMessage(ctx, msg)
	})
}

func TestHandleMessage_CommandChannel_StatusCommand(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, store := newTestOrchestrator(t, bot)

	ctx := context.Background()
	err := store.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RoleEngineer1,
		Status:        coordination.StatusWorking,
		Ticket:        "SQ-99",
		FilesTouching: []string{},
	})
	require.NoError(t, err)

	dispatcher := cli.NewCommandDispatcher(orch, bot)
	msg := slack.IncomingMessage{
		Channel: "commands",
		Text:    "status",
		IsDM:    false,
	}

	assert.NotPanics(t, func() {
		dispatcher.HandleMessage(ctx, msg)
	})
}

func TestHandleMessage_DM_RepliesToDM(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	msg := slack.IncomingMessage{
		Channel:   "commands",
		ChannelID: "D001",
		Text:      "what is going on?",
		IsDM:      true,
	}

	assert.NotPanics(t, func() {
		dispatcher.HandleMessage(ctx, msg)
	})
}

func TestHandleMessage_InvalidCommand_PostsError(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	msg := slack.IncomingMessage{
		Channel: "commands",
		Text:    "nonexistent_command",
		IsDM:    false,
	}

	assert.NotPanics(t, func() {
		dispatcher.HandleMessage(ctx, msg)
	})
}

func TestRouteCommand_StatusCommand_ReturnsAgentStatus(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, store := newTestOrchestrator(t, bot)

	ctx := context.Background()
	err := store.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RolePM,
		Status:        coordination.StatusIdle,
		FilesTouching: []string{},
	})
	require.NoError(t, err)

	dispatcher := cli.NewCommandDispatcher(orch, bot)
	cmd := slack.Command{Name: "status", Args: nil}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "pm")
}

func TestRouteCommand_HealthCommand_ReturnsNominal(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	cmd := slack.Command{Name: "health", Args: nil}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "nominal")
}

func TestRouteCommand_VersionCommand_ReturnsVersion(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	cmd := slack.Command{Name: "version", Args: nil}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "squad0 version")
}

func TestRouteCommand_UnknownCommand_ReturnsAcknowledgement(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	cmd := slack.Command{Name: "assign", Args: []string{"SQ-42", "engineer-1"}}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "acknowledged")
}

func TestRouteCommand_PauseAll_ReturnsAllPaused(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	cmd := slack.Command{Name: "pause", Args: nil}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "paused")
}

func TestRouteCommand_ResumeAll_ReturnsAllResumed(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	cmd := slack.Command{Name: "resume", Args: nil}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "resumed")
}

func TestRouteCommand_PauseSingle_ReturnsAgentPaused(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	cmd := slack.Command{Name: "pause", Args: []string{"engineer-1"}}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "engineer-1")
	assert.Contains(t, result, "paused")
}

func TestRouteCommand_ResumeSingle_ReturnsAgentResumed(t *testing.T) {
	t.Parallel()

	server := newTestSlackServer()
	defer server.Close()

	bot := newTestBot(server.URL)
	orch, _ := newTestOrchestrator(t, bot)
	dispatcher := cli.NewCommandDispatcher(orch, bot)

	ctx := context.Background()
	cmd := slack.Command{Name: "resume", Args: []string{"engineer-2"}}

	result := dispatcher.RouteCommand(ctx, cmd)

	assert.Contains(t, result, "engineer-2")
	assert.Contains(t, result, "resumed")
}

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
