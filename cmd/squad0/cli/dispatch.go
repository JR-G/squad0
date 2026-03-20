package cli

import (
	"context"
	"fmt"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/tui"
)

type commandDispatcher struct {
	orch *orchestrator.Orchestrator
	bot  *slack.Bot
}

func newCommandDispatcher(orch *orchestrator.Orchestrator, bot *slack.Bot) *commandDispatcher {
	return &commandDispatcher{orch: orch, bot: bot}
}

func (dispatcher *commandDispatcher) handleMessage(ctx context.Context, msg slack.IncomingMessage) {
	if msg.IsDM {
		dispatcher.handleDM(ctx, msg)
		return
	}

	if msg.Channel != "commands" {
		return
	}

	cmd, err := slack.ParseCommand(msg.Text)
	if err != nil {
		_ = dispatcher.bot.PostMessage(ctx, "commands", err.Error(), slack.Persona{Name: "Squad0"})
		return
	}

	dispatcher.executeCommand(ctx, cmd)
}

func (dispatcher *commandDispatcher) handleDM(ctx context.Context, msg slack.IncomingMessage) {
	_ = dispatcher.bot.PostAsRole(ctx, msg.ChannelID,
		fmt.Sprintf("Noted. I'll look into: %s", msg.Text),
		agent.RolePM)
}

func (dispatcher *commandDispatcher) executeCommand(ctx context.Context, cmd slack.Command) {
	response := dispatcher.routeCommand(ctx, cmd)
	_ = dispatcher.bot.PostMessage(ctx, "commands", response, slack.Persona{Name: "Squad0"})
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

	return tui.FormatAgentStatus(checkIns, nil)
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
