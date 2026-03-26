package slack_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRecentMessages_ReturnsChronological(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := map[string]interface{}{
			"ok": true,
			"messages": []map[string]string{
				{"user": "U002", "text": "newest message", "ts": "2.0"},
				{"user": "U001", "text": "oldest message", "ts": "1.0"},
			},
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)

	messages, err := bot.LoadRecentMessages(context.Background(), "engineering", 10)

	require.NoError(t, err)
	require.Len(t, messages, 2)
	// Should be reversed to chronological order (oldest first).
	assert.Equal(t, "oldest message", messages[0].Text)
	assert.Equal(t, "U001", messages[0].User)
	assert.Equal(t, "newest message", messages[1].Text)
	assert.Equal(t, "U002", messages[1].User)
}

func TestLoadRecentMessages_UnknownChannel_ReturnsError(t *testing.T) {
	t.Parallel()

	bot := newTestBot(t, slackOKHandler())

	_, err := bot.LoadRecentMessages(context.Background(), "nonexistent", 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown channel")
}

func TestLoadRecentMessages_EmptyHistory_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := map[string]interface{}{
			"ok":       true,
			"messages": []map[string]string{},
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newTestBot(t, handler)

	messages, err := bot.LoadRecentMessages(context.Background(), "engineering", 10)

	require.NoError(t, err)
	assert.Empty(t, messages)
}

func TestLoadRecentMessages_APIError_ReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := map[string]interface{}{
			"ok":    false,
			"error": "channel_not_found",
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	t.Cleanup(server.Close)

	bot := slack.NewBotWithURL(slack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{"engineering": "C001"},
	}, server.URL+"/")

	_, err := bot.LoadRecentMessages(context.Background(), "engineering", 10)

	require.Error(t, err)
}

func TestHistoryMessage_Fields(t *testing.T) {
	t.Parallel()

	msg := slack.HistoryMessage{
		User:      "U123",
		Text:      "hello world",
		Timestamp: "1234.5678",
	}

	assert.Equal(t, "U123", msg.User)
	assert.Equal(t, "hello world", msg.Text)
	assert.Equal(t, "1234.5678", msg.Timestamp)
}
