package ports

import (
	"context"
	"time"
)

// IncomingMessage is a message received from the chat platform.
type IncomingMessage struct {
	Channel    string // logical channel name (e.g. "engineering")
	ChannelID  string // platform-native channel ID
	UserID     string // platform-native author ID
	Username   string // human-readable author name
	Text       string
	Timestamp  string // platform-native message ID for threading
	ThreadTS   string // empty if top-level, parent timestamp otherwise
	IsDM       bool
	ReceivedAt time.Time
}

// MessageHandler is invoked for each incoming message that survives
// the platform's filtering (no bot messages, no subtypes, etc.).
type MessageHandler func(ctx context.Context, msg IncomingMessage)

// SenderIdentity is who a message appears to be from. Different from
// auth identity — squad0 posts via a single bot token but varies the
// display name and avatar per agent persona.
type SenderIdentity struct {
	DisplayName string
	IconURL     string
}

// ChatPlatform is the contract for the team's communication surface
// (Slack today; could be Microsoft Teams / Discord tomorrow).
//
// Squad0 posts as multiple personas through a single bot
// integration, and listens for free-form messages that drive both
// commands and threaded conversations.
type ChatPlatform interface {
	// Post sends a message to a logical channel name as the given
	// persona. Returns the platform-native timestamp so callers can
	// reply in-thread.
	Post(ctx context.Context, channel, text string, sender SenderIdentity) (timestamp string, err error)

	// PostThreadReply posts in-thread under threadTS.
	PostThreadReply(ctx context.Context, channel, threadTS, text string, sender SenderIdentity) error

	// History returns recent messages from a channel, oldest first,
	// up to limit. Used to seed conversation context after restart.
	History(ctx context.Context, channel string, limit int) ([]IncomingMessage, error)

	// Listen registers a handler for incoming messages and starts
	// the event loop. Blocks until ctx is cancelled.
	Listen(ctx context.Context, handler MessageHandler) error
}
