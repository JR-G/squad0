package orchestrator

import (
	"context"
	"log"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
)

// ConversationCoordinator owns the bot, the roster, and the
// optional conversation engine. It serialises the "post a message
// as some agent" pattern so callers don't have to remember to nil-
// check the bot or hand-wire the conversation engine fanout.
//
// Fourth slice of the orchestrator god-object split. Pure forward
// — no state of its own beyond the three pointers it holds.
type ConversationCoordinator struct {
	bot          *slack.Bot
	conversation *ConversationEngine
	roster       map[agent.Role]string
}

// NewConversationCoordinator wires a bot. The roster and
// conversation engine are optional and can be set later via
// SetRoster / SetConversation as the orchestrator wires its
// dependencies.
func NewConversationCoordinator(bot *slack.Bot) *ConversationCoordinator {
	return &ConversationCoordinator{bot: bot}
}

// SetConversation attaches the conversation engine for thread
// notification fanout. Pass nil to disable the fanout.
func (coord *ConversationCoordinator) SetConversation(engine *ConversationEngine) {
	coord.conversation = engine
}

// SetRoster attaches the role→display-name mapping used for
// thread sender attribution.
func (coord *ConversationCoordinator) SetRoster(roster map[agent.Role]string) {
	coord.roster = roster
}

// NameForRole returns the agent's chosen display name, falling
// back to the role slug if no name is known. Safe with a nil
// coordinator or nil roster.
func (coord *ConversationCoordinator) NameForRole(role agent.Role) string {
	if coord == nil || coord.roster == nil {
		return string(role)
	}
	name, ok := coord.roster[role]
	if !ok || name == string(role) {
		return string(role)
	}
	return name
}

// Post sends a message to the named channel as the role's persona,
// then forwards the new thread root to the conversation engine so
// other agents can opt in to respond. No-op when no bot is wired.
func (coord *ConversationCoordinator) Post(ctx context.Context, channel, text string, role agent.Role) {
	if coord == nil || coord.bot == nil {
		return
	}

	ts, err := coord.bot.PostAsRoleWithTS(ctx, channel, text, role)
	if err != nil {
		log.Printf("postAsRole failed for %s in %s: %v", role, channel, err)
		return
	}

	if coord.conversation == nil {
		return
	}
	go coord.conversation.OnThreadMessage(ctx, channel, coord.NameForRole(role), text, ts)
}

// Announce posts a message without triggering the conversation
// engine fanout. Used for status updates and announcements that
// don't need agent responses. No-op when no bot is wired.
func (coord *ConversationCoordinator) Announce(ctx context.Context, channel, text string, role agent.Role) {
	if coord == nil || coord.bot == nil {
		return
	}
	_ = coord.bot.PostAsRole(ctx, channel, text, role)
}
