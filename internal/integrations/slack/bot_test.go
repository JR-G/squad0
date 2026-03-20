package slack_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/stretchr/testify/assert"
)

func TestBot_ChannelName_KnownChannel_ReturnsName(t *testing.T) {
	t.Parallel()

	bot := slack.NewBot(slack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"engineering": "C123",
			"feed":        "C456",
		},
		Personas: map[agent.Role]slack.Persona{},
	})

	name, ok := bot.ChannelName("C123")

	assert.True(t, ok)
	assert.Equal(t, "engineering", name)
}

func TestBot_ChannelName_UnknownChannel_ReturnsFalse(t *testing.T) {
	t.Parallel()

	bot := slack.NewBot(slack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{},
		Personas: map[agent.Role]slack.Persona{},
	})

	_, ok := bot.ChannelName("C999")

	assert.False(t, ok)
}

func TestBot_UpdatePersonas_ReplacesMap(t *testing.T) {
	t.Parallel()

	bot := slack.NewBot(slack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{},
		Personas: map[agent.Role]slack.Persona{},
	})

	newPersonas := map[agent.Role]slack.Persona{
		agent.RolePM: {Role: agent.RolePM, Name: "Nova", IconURL: "https://example.com/nova.png"},
	}

	bot.UpdatePersonas(newPersonas)
}
