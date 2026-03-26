package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/pipeline"
)

// Scheduler runs periodic rituals — standups, retros, and health checks.
type Scheduler struct {
	bot             *slack.Bot
	monitor         *health.Monitor
	alerter         *health.Alerter
	standupInterval time.Duration
	healthInterval  time.Duration
	completedCount  int
	retroThreshold  int
	pipelineStore   *pipeline.WorkItemStore
	agents          map[agent.Role]*agent.Agent
	roster          map[agent.Role]string
}

// SchedulerConfig holds configuration for ritual scheduling.
type SchedulerConfig struct {
	StandupInterval   time.Duration
	HealthInterval    time.Duration
	RetroAfterTickets int
}

// NewScheduler creates a Scheduler with the given dependencies.
func NewScheduler(
	bot *slack.Bot,
	monitor *health.Monitor,
	alerter *health.Alerter,
	cfg SchedulerConfig,
) *Scheduler {
	return &Scheduler{
		bot:             bot,
		monitor:         monitor,
		alerter:         alerter,
		standupInterval: cfg.StandupInterval,
		healthInterval:  cfg.HealthInterval,
		retroThreshold:  cfg.RetroAfterTickets,
	}
}

// SetPipeline connects the pipeline store so standups can report on
// active work items.
func (sched *Scheduler) SetPipeline(store *pipeline.WorkItemStore) {
	sched.pipelineStore = store
}

// SetAgents connects the agent map so the PM can compose standups
// via QuickChat.
func (sched *Scheduler) SetAgents(agents map[agent.Role]*agent.Agent) {
	sched.agents = agents
}

// SetRoster stores the role-to-name mapping so standup summaries use
// chosen agent names.
func (sched *Scheduler) SetRoster(roster map[agent.Role]string) {
	sched.roster = roster
}

// Run starts the ritual scheduling loop. Blocks until context is cancelled.
func (sched *Scheduler) Run(ctx context.Context) error {
	standupTicker := time.NewTicker(sched.standupInterval)
	healthTicker := time.NewTicker(sched.healthInterval)
	defer standupTicker.Stop()
	defer healthTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-standupTicker.C:
			sched.runStandup(ctx)
		case <-healthTicker.C:
			sched.runHealthCheck(ctx)
		}
	}
}

// RecordCompletion increments the completed ticket counter and triggers
// a retro if the threshold is reached.
func (sched *Scheduler) RecordCompletion(ctx context.Context) {
	sched.completedCount++

	if sched.completedCount < sched.retroThreshold {
		return
	}

	sched.completedCount = 0
	sched.runRetro(ctx)
}

// CompletedCount returns the number of completed tickets since the last
// retro.
func (sched *Scheduler) CompletedCount() int {
	return sched.completedCount
}

func (sched *Scheduler) runStandup(ctx context.Context) {
	log.Println("running standup")

	if sched.bot == nil {
		return
	}

	// Build context about what everyone is working on.
	context := sched.buildStandupContext(ctx)

	// If we have the PM agent, have it compose the standup naturally.
	summary := sched.tryPMStandup(ctx, context)
	if summary != "" {
		_ = sched.bot.PostAsRole(ctx, "standup", summary, agent.RolePM)
		return
	}

	// Fallback to health-only summary.
	healthStates := sched.monitor.AllHealth(ctx)
	fallback := health.FormatHealthSummary(healthStates)
	_ = sched.bot.PostAsRole(ctx, "standup", fallback, agent.RolePM)
}

func (sched *Scheduler) tryPMStandup(ctx context.Context, standupContext string) string {
	pmAgent := sched.agentForRole(agent.RolePM)
	if pmAgent == nil || standupContext == "" {
		return ""
	}
	return sched.pmComposedStandup(ctx, pmAgent, standupContext)
}

func (sched *Scheduler) buildStandupContext(ctx context.Context) string {
	engineers := []agent.Role{
		agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3,
	}

	lines := make([]string, 0, len(engineers))
	for _, role := range engineers {
		lines = append(lines, sched.buildEngineerLine(ctx, role))
	}

	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (sched *Scheduler) buildEngineerLine(ctx context.Context, role agent.Role) string {
	name := sched.nameForRole(role)
	items := sched.openItemsForRole(ctx, role)
	healthState := sched.healthStateForRole(role)
	prefix := fmt.Sprintf("- %s (%s): ", name, healthState)

	if len(items) == 0 {
		return prefix + "idle, no active work"
	}

	itemDescs := make([]string, 0, len(items))
	for _, item := range items {
		itemDescs = append(itemDescs, fmt.Sprintf(
			"%s (stage: %s, PR: %s)",
			item.Ticket, item.Stage, prStatus(item.PRURL),
		))
	}
	return prefix + strings.Join(itemDescs, "; ")
}

func prStatus(prURL string) string {
	if prURL == "" {
		return "none"
	}
	return "open"
}

func (sched *Scheduler) pmComposedStandup(ctx context.Context, pmAgent *agent.Agent, standupContext string) string {
	prompt := fmt.Sprintf(
		"You are posting the daily standup to #standup. "+
			"Here is what each engineer is working on:\n\n%s\n\n"+
			"Write a concise standup summary. Use Slack formatting: "+
			"*bold* not **bold**. No markdown headers. Use people's names, "+
			"not role IDs. Keep it brief — 3-6 lines max.",
		standupContext,
	)

	response, err := pmAgent.QuickChat(ctx, prompt)
	if err != nil {
		log.Printf("standup: PM QuickChat failed: %v", err)
		return ""
	}

	return filterPassResponse(response)
}

func (sched *Scheduler) openItemsForRole(ctx context.Context, role agent.Role) []pipeline.WorkItem {
	if sched.pipelineStore == nil {
		return nil
	}

	items, err := sched.pipelineStore.OpenByEngineer(ctx, role)
	if err != nil {
		return nil
	}
	return items
}

func (sched *Scheduler) agentForRole(role agent.Role) *agent.Agent {
	if sched.agents == nil {
		return nil
	}
	return sched.agents[role]
}

func (sched *Scheduler) nameForRole(role agent.Role) string {
	if sched.roster == nil {
		return string(role)
	}

	name, ok := sched.roster[role]
	if !ok || name == "" {
		return string(role)
	}
	return name
}

func (sched *Scheduler) healthStateForRole(role agent.Role) string {
	if sched.monitor == nil {
		return "unknown"
	}

	agentHealth, err := sched.monitor.GetHealth(role)
	if err != nil {
		return "unknown"
	}
	return string(agentHealth.State)
}

func (sched *Scheduler) runHealthCheck(ctx context.Context) {
	if sched.alerter == nil {
		return
	}

	alertCount, err := sched.alerter.CheckAndAlert(ctx)
	if err != nil {
		log.Printf("health check error: %v", err)
		return
	}

	if alertCount > 0 {
		log.Printf("posted %d health alerts", alertCount)
	}
}

func (sched *Scheduler) runRetro(ctx context.Context) {
	log.Println("running retro")

	msg := fmt.Sprintf("*Retro* — %d tickets completed since last retro. Time to reflect on what went well and what to improve.", sched.retroThreshold)

	if sched.bot == nil {
		return
	}

	_ = sched.bot.PostAsRole(ctx, "feed", msg, agent.RolePM)
}
