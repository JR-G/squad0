package slack

import (
	"context"
	"fmt"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// MessageSender posts messages to Slack channels. This interface exists
// to enable testing without a real Slack connection.
type MessageSender interface {
	PostMessage(ctx context.Context, channel, text string, persona Persona) error
	PostThreadReply(ctx context.Context, channel, threadTS, text string, persona Persona) error
}

// Bot manages the Slack connection, message sending with rate limiting,
// and socket mode event handling for CEO commands.
type Bot struct {
	client   *slackapi.Client
	socket   *socketmode.Client
	limiter  *RateLimiter
	personas map[agent.Role]Persona
	channels map[string]string
}

// BotConfig holds the configuration needed to create a Bot.
type BotConfig struct {
	BotToken   string
	AppToken   string
	Channels   map[string]string
	Personas   map[agent.Role]Persona
	MinSpacing time.Duration
}

// NewBot creates a Bot with the given configuration. The channels map
// maps logical names (e.g. "feed", "engineering") to Slack channel IDs.
func NewBot(cfg BotConfig) *Bot {
	client := slackapi.New(
		cfg.BotToken,
		slackapi.OptionAppLevelToken(cfg.AppToken),
	)

	socket := socketmode.New(
		client,
		socketmode.OptionLog(nil),
	)

	return &Bot{
		client:   client,
		socket:   socket,
		limiter:  NewRateLimiter(cfg.MinSpacing),
		personas: cfg.Personas,
		channels: cfg.Channels,
	}
}

// PostMessage sends a message to the named channel as the given persona.
func (bot *Bot) PostMessage(ctx context.Context, channel, text string, persona Persona) error {
	channelID, err := bot.resolveChannel(channel)
	if err != nil {
		return err
	}

	if err := bot.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter wait: %w", err)
	}

	opts := []slackapi.MsgOption{
		slackapi.MsgOptionText(text, false),
		slackapi.MsgOptionUsername(persona.Username),
	}

	if persona.IconURL != "" {
		opts = append(opts, slackapi.MsgOptionIconURL(persona.IconURL))
	}

	_, _, err = bot.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return fmt.Errorf("posting to %s: %w", channel, err)
	}

	return nil
}

// PostThreadReply sends a threaded reply in the named channel.
func (bot *Bot) PostThreadReply(ctx context.Context, channel, threadTS, text string, persona Persona) error {
	channelID, err := bot.resolveChannel(channel)
	if err != nil {
		return err
	}

	if err := bot.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter wait: %w", err)
	}

	opts := []slackapi.MsgOption{
		slackapi.MsgOptionText(text, false),
		slackapi.MsgOptionUsername(persona.Username),
		slackapi.MsgOptionTS(threadTS),
	}

	if persona.IconURL != "" {
		opts = append(opts, slackapi.MsgOptionIconURL(persona.IconURL))
	}

	_, _, err = bot.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return fmt.Errorf("posting thread reply to %s: %w", channel, err)
	}

	return nil
}

// PostAsRole sends a message as the given agent role's persona.
func (bot *Bot) PostAsRole(ctx context.Context, channel, text string, role agent.Role) error {
	persona := PersonaForRole(role, bot.personas)
	return bot.PostMessage(ctx, channel, text, persona)
}

// ChannelID returns the Slack channel ID for the given logical name.
func (bot *Bot) ChannelID(name string) (string, error) {
	return bot.resolveChannel(name)
}

// SocketClient returns the socket mode client for event handling.
func (bot *Bot) SocketClient() *socketmode.Client {
	return bot.socket
}

func (bot *Bot) resolveChannel(name string) (string, error) {
	channelID, ok := bot.channels[name]
	if !ok {
		return "", fmt.Errorf("unknown channel %q", name)
	}
	return channelID, nil
}
