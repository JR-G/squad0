//go:build integration

package slack_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/stretchr/testify/assert"
)

func TestNewBot_CreatesBot(t *testing.T) {
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
