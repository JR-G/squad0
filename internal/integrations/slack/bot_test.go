package slack_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	slackapi "github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBot(t *testing.T, handler http.Handler) *slack.Bot {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return slack.NewBotWithURL(slack.BotConfig{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
		Channels: map[string]string{
			"engineering": "C001",
			"feed":        "C002",
			"commands":    "C003",
		},
		Personas: map[agent.Role]slack.Persona{
			agent.RolePM:        {Role: agent.RolePM, Name: "Nova", IconURL: "https://example.com/nova.png"},
			agent.RoleEngineer1: {Role: agent.RoleEngineer1, Name: "Ada"},
		},
		MinSpacing: 0,
	}, server.URL+"/")
}

func slackOKHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := map[string]interface{}{
			"ok":      true,
			"channel": "C001",
			"ts":      "1234567890.123456",
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})
}

func TestBot_PostMessage_SendsToChannel(t *testing.T) {
	t.Parallel()

	var receivedChannel string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		receivedChannel = req.FormValue("channel")

		resp := map[string]interface{}{"ok": true, "channel": receivedChannel, "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)
	persona := slack.Persona{Name: "Ada", IconURL: "https://example.com/ada.png"}

	err := bot.PostMessage(context.Background(), "engineering", "hello team", persona)

	require.NoError(t, err)
	assert.Equal(t, "C001", receivedChannel)
}

func TestBot_PostMessage_SetsUsername(t *testing.T) {
	t.Parallel()

	var receivedUsername string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		receivedUsername = req.FormValue("username")

		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)
	persona := slack.Persona{Name: "Rex"}

	err := bot.PostMessage(context.Background(), "engineering", "test", persona)

	require.NoError(t, err)
	assert.Equal(t, "Rex", receivedUsername)
}

func TestBot_PostMessage_UnknownChannel_ReturnsError(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t, slackOKHandler())
	persona := slack.Persona{Name: "test"}

	err := bot.PostMessage(context.Background(), "nonexistent", "test", persona)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown channel")
}

func TestBot_PostMessage_SlackError_ReturnsError(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := slackapi.SlackResponse{Ok: false}
		resp.Error = "channel_not_found"
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)
	persona := slack.Persona{Name: "test"}

	err := bot.PostMessage(context.Background(), "engineering", "test", persona)

	require.Error(t, err)
}

func TestBot_PostThreadReply_SetsThreadTS(t *testing.T) {
	t.Parallel()

	var receivedThreadTS string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		receivedThreadTS = req.FormValue("thread_ts")

		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)
	persona := slack.Persona{Name: "Ada"}

	err := bot.PostThreadReply(context.Background(), "engineering", "1234.5678", "reply text", persona)

	require.NoError(t, err)
	assert.Equal(t, "1234.5678", receivedThreadTS)
}

func TestBot_PostAsRole_UsesRolePersona(t *testing.T) {
	t.Parallel()

	var receivedUsername string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		receivedUsername = req.FormValue("username")

		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)

	err := bot.PostAsRole(context.Background(), "engineering", "status update", agent.RolePM)

	require.NoError(t, err)
	assert.Equal(t, "Nova", receivedUsername)
}

func TestBot_PostThreadAsRole_UsesRolePersona(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t, slackOKHandler())

	err := bot.PostThreadAsRole(context.Background(), "engineering", "1234.5678", "reply", agent.RoleEngineer1)

	require.NoError(t, err)
}

func TestBot_PostAsRole_UnknownRole_UsesFallback(t *testing.T) {
	t.Parallel()

	var receivedUsername string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		receivedUsername = req.FormValue("username")

		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)

	err := bot.PostAsRole(context.Background(), "engineering", "test", agent.RoleDesigner)

	require.NoError(t, err)
	assert.Equal(t, "designer", receivedUsername)
}

func TestBot_ChannelName_KnownChannel_ReturnsName(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t, slackOKHandler())

	name, ok := bot.ChannelName("C001")

	assert.True(t, ok)
	assert.Equal(t, "engineering", name)
}

func TestBot_ChannelName_UnknownChannel_ReturnsFalse(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t, slackOKHandler())

	_, ok := bot.ChannelName("C999")

	assert.False(t, ok)
}

func TestBot_UpdatePersonas_ReplacesMap(t *testing.T) {
	t.Parallel()

	var receivedUsername string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		receivedUsername = req.FormValue("username")

		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)
	bot.UpdatePersonas(map[agent.Role]slack.Persona{
		agent.RolePM: {Role: agent.RolePM, Name: "Zara"},
	})

	err := bot.PostAsRole(context.Background(), "engineering", "test", agent.RolePM)

	require.NoError(t, err)
	assert.Equal(t, "Zara", receivedUsername)
}

func TestNewBot_CreatesWithDefaultURL(t *testing.T) {
	t.Parallel()

	bot := slack.NewBot(slack.BotConfig{
		BotToken: "test-token",
		AppToken: "test-app-token",
		Channels: map[string]string{"commands": "C001"},
		Personas: map[agent.Role]slack.Persona{},
	})

	assert.NotNil(t, bot)
	assert.NotNil(t, bot.SocketClient())
}

func TestBot_PostMessage_RateLimiterCancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	bot := slack.NewBotWithURL(slack.BotConfig{
		BotToken:   "xoxb-test",
		AppToken:   "xapp-test",
		Channels:   map[string]string{"feed": "C001"},
		MinSpacing: 10 * time.Second,
	}, "http://localhost:1/")

	persona := slack.Persona{Name: "test"}

	_ = bot.PostMessage(context.Background(), "feed", "first", persona)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bot.PostMessage(ctx, "feed", "second", persona)

	require.Error(t, err)
}
