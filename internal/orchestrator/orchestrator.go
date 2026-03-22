package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
)

// Orchestrator coordinates the squad0 agent team: polls for work via
// the PM, assigns tickets to idle engineers, runs sessions, and
// captures results.
type Orchestrator struct {
	cfg          Config
	agents       map[agent.Role]*agent.Agent
	checkIns     *coordination.CheckInStore
	bot          *slack.Bot
	assigner     *Assigner
	running      bool
	conversation *ConversationEngine
	wg           sync.WaitGroup
}

// Config holds orchestrator-level settings.
type Config struct {
	PollInterval  time.Duration
	MaxParallel   int
	CooldownAfter time.Duration
	WorkEnabled   bool
}

// NewOrchestrator creates an Orchestrator with all dependencies injected.
func NewOrchestrator(
	cfg Config,
	agents map[agent.Role]*agent.Agent,
	checkIns *coordination.CheckInStore,
	bot *slack.Bot,
	assigner *Assigner,
) *Orchestrator {
	return &Orchestrator{
		cfg:      cfg,
		agents:   agents,
		checkIns: checkIns,
		bot:      bot,
		assigner: assigner,
	}
}

// Run starts the main orchestration loop. It blocks until the context
// is cancelled.
func (orch *Orchestrator) Run(ctx context.Context) error {
	orch.running = true
	defer func() { orch.running = false }()

	if err := orch.initialiseCheckIns(ctx); err != nil {
		return fmt.Errorf("initialising check-ins: %w", err)
	}

	log.Println("orchestrator started")
	orch.postAsRole(ctx, "feed", "Squad0 is online. Ready to work.", agent.RolePM)

	ticker := time.NewTicker(orch.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("orchestrator stopping")
			return ctx.Err()
		case <-ticker.C:
			orch.tick(ctx)
		}
	}
}

// IsRunning returns whether the orchestrator loop is active.
func (orch *Orchestrator) IsRunning() bool {
	return orch.running
}

func (orch *Orchestrator) initialiseCheckIns(ctx context.Context) error {
	for role := range orch.agents {
		if err := orch.checkIns.SetIdle(ctx, role); err != nil {
			return fmt.Errorf("setting %s to idle: %w", role, err)
		}
	}
	return nil
}

func (orch *Orchestrator) tick(ctx context.Context) {
	if !orch.cfg.WorkEnabled {
		orch.breakSilence(ctx)
		return
	}

	idleRoles, err := orch.checkIns.IdleAgents(ctx)
	if err != nil {
		log.Printf("error checking idle agents: %v", err)
		return
	}

	idleEngineers := filterEngineers(idleRoles)
	if len(idleEngineers) == 0 {
		return
	}

	assignments, err := orch.assigner.RequestAssignments(ctx, idleEngineers)
	if err != nil {
		log.Printf("error requesting assignments from PM: %v", err)
		orch.breakSilence(ctx)
		return
	}

	if len(assignments) == 0 {
		orch.breakSilence(ctx)
		return
	}

	for _, assignment := range assignments {
		orch.startWork(ctx, assignment)
	}
}

// SetConversationEngine connects the conversation engine to the orchestrator.
func (orch *Orchestrator) SetConversationEngine(engine *ConversationEngine) {
	orch.conversation = engine
}

func (orch *Orchestrator) breakSilence(ctx context.Context) {
	if orch.conversation == nil {
		return
	}
	orch.conversation.BreakSilence(ctx)
}

func (orch *Orchestrator) startWork(ctx context.Context, assignment Assignment) {
	agentInstance, ok := orch.agents[assignment.Role]
	if !ok {
		log.Printf("no agent for role %s", assignment.Role)
		return
	}

	err := orch.checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         assignment.Role,
		Ticket:        assignment.Ticket,
		Status:        coordination.StatusWorking,
		FilesTouching: []string{},
		Message:       fmt.Sprintf("working on %s", assignment.Ticket),
	})
	if err != nil {
		log.Printf("error updating checkin for %s: %v", assignment.Role, err)
		return
	}

	orch.wg.Add(1)
	go func() {
		defer orch.wg.Done()
		orch.runSession(ctx, agentInstance, assignment)
	}()
}

// Wait blocks until all running sessions complete.
func (orch *Orchestrator) Wait() {
	orch.wg.Wait()
}

func (orch *Orchestrator) runSession(ctx context.Context, agentInstance *agent.Agent, assignment Assignment) {
	role := agentInstance.Role()

	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Starting work on %s: %s", assignment.Ticket, assignment.Description),
		role)

	result, err := agentInstance.ExecuteTask(ctx, assignment.Description, nil, assignment.WorkingDir)
	if err != nil {
		log.Printf("session error for %s on %s: %v", role, assignment.Ticket, err)
		orch.postAsRole(ctx, "engineering",
			fmt.Sprintf("Hit an issue with %s: %v", assignment.Ticket, err),
			role)
	}

	if result.Transcript != "" {
		orch.postAsRole(ctx, "engineering",
			fmt.Sprintf("Finished %s.", assignment.Ticket),
			role)
	}

	_ = orch.checkIns.SetIdle(ctx, role)
}

func (orch *Orchestrator) postAsRole(ctx context.Context, channel, text string, role agent.Role) {
	if orch.bot == nil {
		return
	}
	_ = orch.bot.PostAsRole(ctx, channel, text, role)
}

func filterEngineers(roles []agent.Role) []agent.Role {
	engineerRoles := map[agent.Role]bool{
		agent.RoleEngineer1: true,
		agent.RoleEngineer2: true,
		agent.RoleEngineer3: true,
	}

	engineers := make([]agent.Role, 0, len(roles))
	for _, role := range roles {
		if engineerRoles[role] {
			engineers = append(engineers, role)
		}
	}

	return engineers
}
