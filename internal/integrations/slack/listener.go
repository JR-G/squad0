package slack

import (
	"context"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// EventAcknowledger acknowledges socket mode events.
type EventAcknowledger interface {
	Ack(req socketmode.Request, payload ...interface{})
}

// ListenForEvents starts the socket mode event loop, dispatching incoming
// messages to the registered handler. This blocks until the context is
// cancelled.
func (bot *Bot) ListenForEvents(ctx context.Context) error {
	go bot.handleEvents(ctx)
	return bot.socket.RunContext(ctx)
}

func (bot *Bot) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-bot.socket.Events:
			bot.DispatchSocketEvent(ctx, bot.socket, event)
		}
	}
}

// DispatchSocketEvent routes a socket mode event to the appropriate
// handler and acknowledges the request.
func (bot *Bot) DispatchSocketEvent(ctx context.Context, acker EventAcknowledger, event socketmode.Event) {
	switch event.Type { //nolint:exhaustive // only EventsAPI carries messages we need
	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := event.Data.(slackevents.EventsAPIEvent)
		if !ok {
			AckRequest(acker, event.Request)
			return
		}
		AckRequest(acker, event.Request)
		bot.HandleEventsAPIEvent(ctx, eventsAPIEvent)
	default:
		AckRequest(acker, event.Request)
	}
}

// HandleEventsAPIEvent processes a Slack Events API event. Use this to
// dispatch events received outside of socket mode (e.g. HTTP webhooks).
func (bot *Bot) HandleEventsAPIEvent(ctx context.Context, event slackevents.EventsAPIEvent) {
	switch innerEvent := event.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		bot.HandleMessageEvent(ctx, innerEvent)
	default:
		return
	}
}

// HandleMessageEvent processes a single Slack message event, routing it
// to the registered message handler.
func (bot *Bot) HandleMessageEvent(ctx context.Context, event *slackevents.MessageEvent) {
	if bot.onMessage == nil {
		return
	}

	if event.BotID != "" {
		return
	}

	channelName, known := bot.ChannelName(event.Channel)
	isDM := isDirectMessage(event.ChannelType)

	if !known && !isDM {
		return
	}

	msg := IncomingMessage{
		Channel:   channelName,
		ChannelID: event.Channel,
		User:      event.User,
		Text:      event.Text,
		ThreadTS:  event.ThreadTimeStamp,
		IsDM:      isDM,
	}

	bot.onMessage(ctx, msg)
}

// AckRequest acknowledges a socket mode request if non-nil.
func AckRequest(acker EventAcknowledger, req *socketmode.Request) {
	if req == nil {
		return
	}
	acker.Ack(*req)
}

func isDirectMessage(channelType string) bool {
	return channelType == string(slackapi.TYPE_IM)
}
