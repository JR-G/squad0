package slack

import (
	"context"
	"log"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// EventAcknowledger acknowledges socket mode events.
type EventAcknowledger interface {
	Ack(req socketmode.Request, payload ...interface{})
}

// ListenForEvents starts the socket mode event loop using the official
// SocketmodeHandler pattern. Blocks until the context is cancelled.
func (bot *Bot) ListenForEvents(ctx context.Context) error {
	handler := socketmode.NewSocketmodeHandler(bot.socket)
	handler.Handle(socketmode.EventTypeEventsAPI, bot.MakeEventsAPIHandler(ctx))
	handler.HandleDefault(MakeDefaultHandler())
	return handler.RunEventLoopContext(ctx)
}

// MakeEventsAPIHandler returns a socketmode handler function for
// events_api events.
func (bot *Bot) MakeEventsAPIHandler(ctx context.Context) func(*socketmode.Event, *socketmode.Client) {
	return func(event *socketmode.Event, client *socketmode.Client) {
		eventsAPIEvent, ok := event.Data.(slackevents.EventsAPIEvent)
		if !ok {
			log.Printf("unexpected event data type: %T", event.Data)
			client.Ack(*event.Request)
			return
		}

		client.Ack(*event.Request)
		bot.HandleEventsAPIEvent(ctx, eventsAPIEvent)
	}
}

// MakeDefaultHandler returns a socketmode handler that logs unhandled events.
func MakeDefaultHandler() func(*socketmode.Event, *socketmode.Client) {
	return func(event *socketmode.Event, _ *socketmode.Client) {
		log.Printf("socket event: %s", event.Type)
	}
}

// DispatchSocketEvent routes a socket mode event for testing.
func (bot *Bot) DispatchSocketEvent(ctx context.Context, acker EventAcknowledger, event socketmode.Event) {
	switch event.Type { //nolint:exhaustive // only EventsAPI carries messages
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

// HandleEventsAPIEvent processes a Slack Events API event.
func (bot *Bot) HandleEventsAPIEvent(ctx context.Context, event slackevents.EventsAPIEvent) {
	switch innerEvent := event.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		bot.HandleMessageEvent(ctx, innerEvent)
	default:
		return
	}
}

// HandleMessageEvent processes a single Slack message event.
func (bot *Bot) HandleMessageEvent(ctx context.Context, event *slackevents.MessageEvent) {
	if bot.onMessage == nil {
		return
	}

	if event.BotID != "" || event.SubType == "bot_message" {
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
