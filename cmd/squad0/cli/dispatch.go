package cli

import (
	"context"
	"fmt"
	"log"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/orchestrator"
)

type commandDispatcher struct {
	orch         *orchestrator.Orchestrator
	bot          *slack.Bot
	conversation *orchestrator.ConversationEngine
	personas     map[agent.Role]slack.Persona
	links        slack.LinkConfig
}

func newCommandDispatcher(
	orch *orchestrator.Orchestrator,
	bot *slack.Bot,
	conversation *orchestrator.ConversationEngine,
	personas map[agent.Role]slack.Persona,
	links slack.LinkConfig,
) *commandDispatcher {
	return &commandDispatcher{orch: orch, bot: bot, conversation: conversation, personas: personas, links: links}
}

func (dispatcher *commandDispatcher) handleMessage(ctx context.Context, msg slack.IncomingMessage) {
	log.Printf("message received: channel=%s text=%q isDM=%v", msg.Channel, msg.Text, msg.IsDM)

	// Commands and DMs have priority — process immediately, never queued.
	if msg.IsDM {
		dispatcher.handleDM(ctx, msg)
		return
	}

	if msg.Channel == "commands" {
		dispatcher.handleCommand(ctx, msg.Text)
		return
	}

	// Conversation messages run async so they don't block commands.
	if dispatcher.conversation == nil {
		return
	}

	threadTS := msg.ThreadTS
	if threadTS == "" {
		threadTS = msg.Timestamp
	}

	go dispatcher.conversation.OnThreadMessage(ctx, msg.Channel, msg.User, msg.Text, threadTS)
}

func (dispatcher *commandDispatcher) handleCommand(ctx context.Context, text string) {
	cmd, err := slack.ParseCommand(text)
	if err != nil {
		dispatcher.reply(ctx, err.Error())
		return
	}

	response := dispatcher.routeCommand(ctx, cmd)
	dispatcher.reply(ctx, response)
}

func (dispatcher *commandDispatcher) handleDM(ctx context.Context, msg slack.IncomingMessage) {
	err := dispatcher.bot.PostAsRole(ctx, msg.ChannelID,
		fmt.Sprintf("Noted. I'll look into: %s", msg.Text),
		agent.RolePM)
	if err != nil {
		log.Printf("error posting DM reply: %v", err)
	}
}

func (dispatcher *commandDispatcher) reply(ctx context.Context, text string) {
	err := dispatcher.bot.PostMessage(ctx, "commands", text, slack.Persona{Name: "squad0"})
	if err != nil {
		log.Printf("error posting to commands: %v", err)
	}
}

func (dispatcher *commandDispatcher) routeCommand(ctx context.Context, cmd slack.Command) string {
	switch slack.CommandType(cmd.Name) { //nolint:exhaustive // unimplemented commands get default
	case slack.CommandStatus:
		return dispatcher.handleStatus(ctx)
	case slack.CommandStop:
		return handlePauseResume(ctx, dispatcher.orch.PauseAgent, dispatcher.orch.PauseAll, nil, "paused")
	case slack.CommandStart:
		return handlePauseResume(ctx, dispatcher.orch.ResumeAgent, dispatcher.orch.ResumeAll, nil, "resumed")
	case slack.CommandPause:
		return handlePauseResume(ctx, dispatcher.orch.PauseAgent, dispatcher.orch.PauseAll, cmd.Args, "paused")
	case slack.CommandResume:
		return handlePauseResume(ctx, dispatcher.orch.ResumeAgent, dispatcher.orch.ResumeAll, cmd.Args, "resumed")
	case slack.CommandHealth:
		return "Health check: all systems nominal."
	case slack.CommandVersion:
		return fmt.Sprintf("squad0 version %s", version)
	default:
		return fmt.Sprintf("Command `%s` acknowledged.", cmd.Name)
	}
}

func (dispatcher *commandDispatcher) handleStatus(ctx context.Context) string {
	checkIns, err := dispatcher.orch.Status(ctx)
	if err != nil {
		return fmt.Sprintf("Error getting status: %v", err)
	}

	return slack.FormatStatusWithLinks(checkIns, dispatcher.personas, dispatcher.links)
}

func handlePauseResume(
	ctx context.Context,
	singleFn func(context.Context, agent.Role) error,
	allFn func(context.Context) error,
	args []string,
	action string,
) string {
	if len(args) != 0 {
		return pauseResumeSingle(ctx, singleFn, agent.Role(args[0]), action)
	}

	err := allFn(ctx)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	return fmt.Sprintf("All agents %s.", action)
}

func pauseResumeSingle(ctx context.Context, fn func(context.Context, agent.Role) error, role agent.Role, action string) string {
	err := fn(ctx, role)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	return fmt.Sprintf("%s %s.", role, action)
}
