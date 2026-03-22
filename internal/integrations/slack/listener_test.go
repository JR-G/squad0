package slack_test

import (
	"context"
	"sync"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAcker struct {
	acked int
}

func (fa *fakeAcker) Ack(_ socketmode.Request, _ ...interface{}) {
	fa.acked++
}

func newListenerTestBot(t *testing.T) (*islack.Bot, *messageCollector) {
	t.Helper()

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "test-token",
		AppToken: "test-app-token",
		Channels: map[string]string{
			"commands":    "C001",
			"engineering": "C002",
		},
		Personas: map[agent.Role]islack.Persona{},
	}, "http://localhost:1/")

	collector := &messageCollector{}
	bot.OnMessage(collector.handle)

	return bot, collector
}

type messageCollector struct {
	mu       sync.Mutex
	messages []islack.IncomingMessage
}

func (mc *messageCollector) handle(_ context.Context, msg islack.IncomingMessage) {
	mc.mu.Lock()
	mc.messages = append(mc.messages, msg)
	mc.mu.Unlock()
}

func (mc *messageCollector) get() []islack.IncomingMessage {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	result := make([]islack.IncomingMessage, len(mc.messages))
	copy(result, mc.messages)
	return result
}

func TestHandleMessageEvent_UserMessage_DispatchesToHandler(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	bot.HandleMessageEvent(context.Background(), &slackevents.MessageEvent{
		Channel:     "C002",
		User:        "U001",
		Text:        "what are you working on?",
		ChannelType: "channel",
	})

	messages := collector.get()
	require.Len(t, messages, 1)
	assert.Equal(t, "what are you working on?", messages[0].Text)
	assert.Equal(t, "engineering", messages[0].Channel)
	assert.Equal(t, "U001", messages[0].User)
	assert.False(t, messages[0].IsDM)
}

func TestHandleMessageEvent_BotMessage_Ignored(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	bot.HandleMessageEvent(context.Background(), &slackevents.MessageEvent{
		Channel: "C002",
		BotID:   "B001",
		Text:    "automated message",
	})

	assert.Empty(t, collector.get())
}

func TestHandleMessageEvent_DM_DispatchedWithIsDMTrue(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	bot.HandleMessageEvent(context.Background(), &slackevents.MessageEvent{
		Channel:     "D001",
		User:        "U001",
		Text:        "hey PM, reprioritise auth work",
		ChannelType: "im",
	})

	messages := collector.get()
	require.Len(t, messages, 1)
	assert.True(t, messages[0].IsDM)
	assert.Equal(t, "D001", messages[0].ChannelID)
}

func TestHandleMessageEvent_UnknownChannel_NotDM_Ignored(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	bot.HandleMessageEvent(context.Background(), &slackevents.MessageEvent{
		Channel:     "C999",
		User:        "U001",
		Text:        "message in unknown channel",
		ChannelType: "channel",
	})

	assert.Empty(t, collector.get())
}

func TestHandleMessageEvent_ThreadReply_IncludesThreadTS(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	bot.HandleMessageEvent(context.Background(), &slackevents.MessageEvent{
		Channel:         "C002",
		User:            "U001",
		Text:            "good point, let's do that",
		ThreadTimeStamp: "1234567890.123456",
		ChannelType:     "channel",
	})

	messages := collector.get()
	require.Len(t, messages, 1)
	assert.Equal(t, "1234567890.123456", messages[0].ThreadTS)
}

func TestHandleMessageEvent_NoHandler_DoesNotPanic(t *testing.T) {
	t.Parallel()

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "test-token",
		AppToken: "test-app-token",
		Channels: map[string]string{"commands": "C001"},
	}, "http://localhost:1/")

	assert.NotPanics(t, func() {
		bot.HandleMessageEvent(context.Background(), &slackevents.MessageEvent{
			Channel: "C001",
			User:    "U001",
			Text:    "hello",
		})
	})
}

func TestHandleEventsAPIEvent_MessageEvent_Dispatches(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	event := slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Data: &slackevents.MessageEvent{
				Channel:     "C002",
				User:        "U001",
				Text:        "via events API",
				ChannelType: "channel",
			},
		},
	}

	bot.HandleEventsAPIEvent(context.Background(), event)

	messages := collector.get()
	require.Len(t, messages, 1)
	assert.Equal(t, "via events API", messages[0].Text)
}

func TestHandleEventsAPIEvent_NonMessageEvent_Ignored(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	event := slackevents.EventsAPIEvent{
		Type: slackevents.CallbackEvent,
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Data: &slackevents.AppMentionEvent{
				Channel: "C002",
				User:    "U001",
				Text:    "app mention",
			},
		},
	}

	bot.HandleEventsAPIEvent(context.Background(), event)

	assert.Empty(t, collector.get())
}

func TestBot_SocketClient_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "test-token",
		AppToken: "test-app-token",
		Channels: map[string]string{},
	}, "http://localhost:1/")

	assert.NotNil(t, bot.SocketClient())
}

func TestDispatchSocketEvent_EventsAPI_DispatchesAndAcks(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)
	acker := &fakeAcker{}

	event := socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Data: slackevents.EventsAPIEvent{
			Type: slackevents.CallbackEvent,
			InnerEvent: slackevents.EventsAPIInnerEvent{
				Data: &slackevents.MessageEvent{
					Channel:     "C002",
					User:        "U001",
					Text:        "dispatched",
					ChannelType: "channel",
				},
			},
		},
		Request: &socketmode.Request{},
	}

	bot.DispatchSocketEvent(context.Background(), acker, event)

	messages := collector.get()
	require.Len(t, messages, 1)
	assert.Equal(t, "dispatched", messages[0].Text)
	assert.Equal(t, 1, acker.acked)
}

func TestDispatchSocketEvent_NonEventsAPI_AcksOnly(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)
	acker := &fakeAcker{}

	event := socketmode.Event{
		Type:    socketmode.EventTypeHello,
		Request: &socketmode.Request{},
	}

	bot.DispatchSocketEvent(context.Background(), acker, event)

	assert.Empty(t, collector.get())
	assert.Equal(t, 1, acker.acked)
}

func TestDispatchSocketEvent_InvalidEventsAPIData_AcksOnly(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)
	acker := &fakeAcker{}

	event := socketmode.Event{
		Type:    socketmode.EventTypeEventsAPI,
		Data:    "not an EventsAPIEvent",
		Request: &socketmode.Request{},
	}

	bot.DispatchSocketEvent(context.Background(), acker, event)

	assert.Empty(t, collector.get())
	assert.Equal(t, 1, acker.acked)
}

func TestDispatchSocketEvent_NilRequest_DoesNotPanic(t *testing.T) {
	t.Parallel()

	bot, _ := newListenerTestBot(t)
	acker := &fakeAcker{}

	event := socketmode.Event{
		Type:    socketmode.EventTypeConnected,
		Request: nil,
	}

	assert.NotPanics(t, func() {
		bot.DispatchSocketEvent(context.Background(), acker, event)
	})
	assert.Equal(t, 0, acker.acked)
}

func TestMakeEventsAPIHandler_ValidEvent_Dispatches(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	handler := bot.MakeEventsAPIHandler(context.Background())

	event := &socketmode.Event{
		Data: slackevents.EventsAPIEvent{
			Type: slackevents.CallbackEvent,
			InnerEvent: slackevents.EventsAPIInnerEvent{
				Data: &slackevents.MessageEvent{
					Channel:     "C002",
					User:        "U001",
					Text:        "from handler",
					ChannelType: "channel",
				},
			},
		},
		Request: &socketmode.Request{EnvelopeID: "test-123"},
	}

	handler(event, bot.SocketClient())

	messages := collector.get()
	require.Len(t, messages, 1)
	assert.Equal(t, "from handler", messages[0].Text)
}

func TestMakeEventsAPIHandler_InvalidData_AcksOnly(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	handler := bot.MakeEventsAPIHandler(context.Background())

	event := &socketmode.Event{
		Data:    "not an EventsAPIEvent",
		Request: &socketmode.Request{EnvelopeID: "test-456"},
	}

	handler(event, bot.SocketClient())

	assert.Empty(t, collector.get())
}

func TestMakeDefaultHandler_DoesNotPanic(t *testing.T) {
	t.Parallel()

	handler := islack.MakeDefaultHandler()

	event := &socketmode.Event{Type: socketmode.EventTypeHello}

	assert.NotPanics(t, func() {
		handler(event, nil)
	})
}

func TestHandleMessageEvent_BotSubtype_Ignored(t *testing.T) {
	t.Parallel()

	bot, collector := newListenerTestBot(t)

	bot.HandleMessageEvent(context.Background(), &slackevents.MessageEvent{
		Channel: "C002",
		User:    "",
		Text:    "bot said this",
		SubType: "bot_message",
	})

	assert.Empty(t, collector.get())
}

func TestAckRequest_NilRequest_DoesNotPanic(t *testing.T) {
	t.Parallel()

	acker := &fakeAcker{}
	islack.AckRequest(acker, nil)

	assert.Equal(t, 0, acker.acked)
}

func TestAckRequest_ValidRequest_Acks(t *testing.T) {
	t.Parallel()

	acker := &fakeAcker{}
	islack.AckRequest(acker, &socketmode.Request{})

	assert.Equal(t, 1, acker.acked)
}
