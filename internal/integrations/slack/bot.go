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

// IncomingMessage represents a message received from Slack.
type IncomingMessage struct {
	Channel   string
	ChannelID string
	User      string
	Text      string
	Timestamp string // Message timestamp — used as thread root for replies
	ThreadTS  string // Non-empty when message is itself a thread reply
	IsDM      bool
}

// MessageHandler is called when a message is received from Slack.
type MessageHandler func(ctx context.Context, msg IncomingMessage)

// Bot manages the Slack connection, message sending with rate limiting,
// and socket mode event handling for CEO commands.
type Bot struct {
	client          *slackapi.Client
	socket          *socketmode.Client
	limiter         *RateLimiter
	personas        map[agent.Role]Persona
	channels        map[string]string
	reverseChannels map[string]string
	onMessage       MessageHandler
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
	return newBot(cfg, client)
}

// NewBotWithURL creates a Bot pointing at a custom Slack API URL. Pass
// an httptest server URL to test without a real Slack connection.
func NewBotWithURL(cfg BotConfig, apiURL string) *Bot {
	client := slackapi.New(
		cfg.BotToken,
		slackapi.OptionAPIURL(apiURL),
	)
	return newBot(cfg, client)
}

func newBot(cfg BotConfig, client *slackapi.Client) *Bot {
	socket := socketmode.New(client)

	reverse := make(map[string]string, len(cfg.Channels))
	for name, channelID := range cfg.Channels {
		reverse[channelID] = name
	}

	return &Bot{
		client:          client,
		socket:          socket,
		limiter:         NewRateLimiter(cfg.MinSpacing),
		personas:        cfg.Personas,
		channels:        cfg.Channels,
		reverseChannels: reverse,
	}
}

// OnMessage registers a handler for incoming messages.
func (bot *Bot) OnMessage(handler MessageHandler) {
	bot.onMessage = handler
}

// UpdatePersonas replaces the current persona map. Called when agents
// choose or update their names.
func (bot *Bot) UpdatePersonas(personas map[agent.Role]Persona) {
	bot.personas = personas
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

	opts := buildMessageOpts(text, persona)

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

	opts := buildMessageOpts(text, persona)
	opts = append(opts, slackapi.MsgOptionTS(threadTS))

	_, _, err = bot.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return fmt.Errorf("posting thread reply to %s: %w", channel, err)
	}

	return nil
}

// PostAsRole sends a message as the given agent role's persona.
func (bot *Bot) PostAsRole(ctx context.Context, channel, text string, role agent.Role) error {
	persona := bot.personaForRole(role)
	return bot.PostMessage(ctx, channel, text, persona)
}

// PostAsRoleWithTS sends a message and returns the Slack timestamp so
// callers can start threads from it.
func (bot *Bot) PostAsRoleWithTS(ctx context.Context, channel, text string, role agent.Role) (string, error) {
	persona := bot.personaForRole(role)

	channelID, err := bot.resolveChannel(channel)
	if err != nil {
		return "", err
	}

	if err := bot.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter wait: %w", err)
	}

	opts := buildMessageOpts(text, persona)

	_, ts, err := bot.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return "", fmt.Errorf("posting to %s: %w", channel, err)
	}

	return ts, nil
}

// PostThreadAsRole sends a threaded reply as the given agent role's persona.
func (bot *Bot) PostThreadAsRole(ctx context.Context, channel, threadTS, text string, role agent.Role) error {
	persona := bot.personaForRole(role)
	return bot.PostThreadReply(ctx, channel, threadTS, text, persona)
}

// ChannelName returns the logical channel name for a Slack channel ID.
func (bot *Bot) ChannelName(channelID string) (string, bool) {
	name, ok := bot.reverseChannels[channelID]
	return name, ok
}

// SocketClient returns the socket mode client for event handling.
func (bot *Bot) SocketClient() *socketmode.Client {
	return bot.socket
}

func (bot *Bot) personaForRole(role agent.Role) Persona {
	persona, ok := bot.personas[role]
	if !ok {
		return Persona{Role: role, Name: string(role), IconURL: GenerateIdenticonURL(string(role))}
	}
	return persona
}

func (bot *Bot) resolveChannel(name string) (string, error) {
	channelID, ok := bot.channels[name]
	if !ok {
		return "", fmt.Errorf("unknown channel %q", name)
	}
	return channelID, nil
}

// HistoryMessage represents a message retrieved from Slack channel history.
type HistoryMessage struct {
	User      string
	Text      string
	Timestamp string
}

// LoadRecentMessages calls conversations.history to load recent messages
// from a channel. Returns up to limit messages, most recent first.
func (bot *Bot) LoadRecentMessages(ctx context.Context, channel string, limit int) ([]HistoryMessage, error) {
	channelID, err := bot.resolveChannel(channel)
	if err != nil {
		return nil, err
	}

	params := &slackapi.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	}

	history, err := bot.client.GetConversationHistoryContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("loading history for %s: %w", channel, err)
	}

	messages := make([]HistoryMessage, 0, len(history.Messages))
	// Slack returns newest first — reverse to chronological order.
	for idx := len(history.Messages) - 1; idx >= 0; idx-- {
		msg := history.Messages[idx]
		messages = append(messages, HistoryMessage{
			User:      resolveHistoryAuthor(msg),
			Text:      msg.Text,
			Timestamp: msg.Timestamp,
		})
	}

	return messages, nil
}

// resolveHistoryAuthor extracts the best human-readable author for a
// message pulled from Slack history. Squad0's agents post via the
// shared bot token with a per-message username override, so every
// message returns the same raw User ID. Preferring Msg.Username and
// BotProfile.Name restores the persona display name ("Morgan — PM",
// "Callum — Engineer") so the seeded chat history is meaningful and
// the model doesn't have to invent labels like "Engineer-1" to
// distinguish agents.
func resolveHistoryAuthor(msg slackapi.Message) string {
	if msg.Username != "" {
		return msg.Username
	}
	if msg.BotProfile != nil && msg.BotProfile.Name != "" {
		return msg.BotProfile.Name
	}
	return msg.User
}

func buildMessageOpts(text string, persona Persona) []slackapi.MsgOption {
	opts := []slackapi.MsgOption{
		slackapi.MsgOptionText(text, false),
		slackapi.MsgOptionUsername(persona.DisplayName()),
	}

	if persona.IconURL != "" {
		opts = append(opts, slackapi.MsgOptionIconURL(persona.IconURL))
	}

	return opts
}
