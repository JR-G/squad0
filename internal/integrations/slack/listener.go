package slack

import (
	"context"
	"log"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

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
			bot.dispatchEvent(ctx, event)
		}
	}
}

func (bot *Bot) dispatchEvent(ctx context.Context, event socketmode.Event) {
	switch event.Type { //nolint:exhaustive // only EventsAPI carries messages we need to handle
	case socketmode.EventTypeEventsAPI:
		bot.handleEventsAPI(ctx, event)
	default:
		if event.Request != nil {
			bot.socket.Ack(*event.Request)
		}
	}
}

func (bot *Bot) handleEventsAPI(ctx context.Context, event socketmode.Event) {
	eventsAPIEvent, ok := event.Data.(slackevents.EventsAPIEvent)
	if !ok {
		bot.socket.Ack(*event.Request)
		return
	}

	bot.socket.Ack(*event.Request)

	switch innerEvent := eventsAPIEvent.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		bot.handleMessageEvent(ctx, innerEvent)
	default:
		log.Printf("unhandled inner event type: %T", innerEvent)
	}
}

func (bot *Bot) handleMessageEvent(ctx context.Context, event *slackevents.MessageEvent) {
	if bot.onMessage == nil {
		return
	}

	if event.BotID != "" {
		return
	}

	channelName, known := bot.ChannelName(event.Channel)
	isDM := isDirectMessage(event.ChannelType)

	msg := IncomingMessage{
		Channel:   channelName,
		ChannelID: event.Channel,
		User:      event.User,
		Text:      event.Text,
		ThreadTS:  event.ThreadTimeStamp,
		IsDM:      isDM,
	}

	if !known && !isDM {
		return
	}

	bot.onMessage(ctx, msg)
}

func isDirectMessage(channelType string) bool {
	return channelType == string(slackapi.TYPE_IM)
}
