package slack_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	slackapi "github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBot_PostThreadReply_UnknownChannel_ReturnsError(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t, slackOKHandler())
	persona := slack.Persona{Name: "test"}

	err := bot.PostThreadReply(context.Background(), "nonexistent", "1234.5678", "reply", persona)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown channel")
}

func TestBot_PostThreadReply_RateLimiterCancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	bot := slack.NewBotWithURL(slack.BotConfig{
		BotToken:   "xoxb-test",
		AppToken:   "xapp-test",
		Channels:   map[string]string{"feed": "C001"},
		MinSpacing: 10 * time.Second,
	}, "http://localhost:1/")

	persona := slack.Persona{Name: "test"}

	_ = bot.PostThreadReply(context.Background(), "feed", "1234.5678", "first", persona)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bot.PostThreadReply(ctx, "feed", "1234.5678", "second", persona)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limiter")
}

func TestBot_PostThreadReply_SlackError_ReturnsError(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := slackapi.SlackResponse{Ok: false}
		resp.Error = "thread_not_found"
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)
	persona := slack.Persona{Name: "test"}

	err := bot.PostThreadReply(context.Background(), "engineering", "1234.5678", "reply", persona)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "posting thread reply")
}

func TestBot_PostThreadAsRole_UnknownChannel_ReturnsError(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t, slackOKHandler())

	err := bot.PostThreadAsRole(context.Background(), "nonexistent", "1234.5678", "reply", agent.RolePM)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown channel")
}
