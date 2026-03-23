package orchestrator

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
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
	monitor      *health.Monitor
	running      bool
	conversation *ConversationEngine
	wg           sync.WaitGroup
	assigning    bool
	assigningMu  sync.Mutex

	// Per-agent session cancellation. When pause is called, the cancel
	// function fires and the running Claude Code process is killed.
	sessionCancels map[agent.Role]context.CancelFunc
	cancelsMu      sync.Mutex
}

// Config holds orchestrator-level settings.
type Config struct {
	PollInterval     time.Duration
	MaxParallel      int
	CooldownAfter    time.Duration
	WorkEnabled      bool
	TargetRepoDir    string
	MemoryBinaryPath string
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
		cfg:            cfg,
		agents:         agents,
		checkIns:       checkIns,
		bot:            bot,
		assigner:       assigner,
		sessionCancels: make(map[agent.Role]context.CancelFunc),
	}
}

// SetHealthMonitor connects the health monitor for health-aware
// assignment and session tracking.
func (orch *Orchestrator) SetHealthMonitor(monitor *health.Monitor) {
	orch.monitor = monitor
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
	log.Printf("tick: work_enabled=%v", orch.cfg.WorkEnabled)

	if !orch.cfg.WorkEnabled {
		orch.breakSilence(ctx)
		return
	}

	idleRoles, err := orch.checkIns.IdleAgents(ctx)
	if err != nil {
		log.Printf("error checking idle agents: %v", err)
		return
	}

	log.Printf("tick: %d idle agents, roles: %v", len(idleRoles), idleRoles)

	idleEngineers := orch.filterHealthyEngineers(idleRoles)
	if len(idleEngineers) == 0 {
		log.Println("tick: no idle engineers")
		return
	}

	orch.assigningMu.Lock()
	if orch.assigning {
		orch.assigningMu.Unlock()
		log.Println("tick: assignment already in progress, skipping")
		return
	}
	orch.assigning = true
	orch.assigningMu.Unlock()

	log.Printf("tick: requesting assignments for %v", idleEngineers)

	go func() {
		defer func() {
			orch.assigningMu.Lock()
			orch.assigning = false
			orch.assigningMu.Unlock()
		}()

		assignments, assignErr := orch.assigner.RequestAssignments(ctx, idleEngineers)
		if assignErr != nil {
			log.Printf("tick: assignment failed: %v", assignErr)
			return
		}

		log.Printf("tick: got %d assignments", len(assignments))

		if len(assignments) == 0 {
			return
		}

		for _, assignment := range assignments {
			log.Printf("tick: assigning %s to %s", assignment.Ticket, assignment.Role)
			orch.startWork(ctx, assignment)
		}
	}()
}

// SetConversationEngine connects the conversation engine to the orchestrator
// and wires up the pause checker so paused agents stay silent.
func (orch *Orchestrator) SetConversationEngine(engine *ConversationEngine) {
	orch.conversation = engine
	engine.SetPauseChecker(orch.IsPaused)
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

	// Create a cancellable context for this session so pause can kill it.
	sessionCtx, cancel := context.WithCancel(ctx)
	orch.registerSessionCancel(assignment.Role, cancel)

	go MoveTicketState(ctx, orch.agents[agent.RolePM], assignment.Ticket, "In Progress")

	orch.wg.Add(1)
	go func() {
		defer orch.wg.Done()
		defer orch.clearSessionCancel(assignment.Role)
		orch.runSession(sessionCtx, agentInstance, assignment)
	}()
}

// Wait blocks until all running sessions complete.
func (orch *Orchestrator) Wait() {
	orch.wg.Wait()
}

func (orch *Orchestrator) runSession(ctx context.Context, agentInstance *agent.Agent, assignment Assignment) {
	role := agentInstance.Role()

	orch.recordSessionStart(role)

	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Picking up %s: %s", assignment.Ticket, assignment.Description),
		role)

	workSession, err := NewWorkSession(ctx, orch.cfg.TargetRepoDir, role, assignment.Ticket)
	if err != nil {
		log.Printf("worktree creation failed for %s: %v", role, err)
		orch.postAsRole(ctx, "engineering",
			fmt.Sprintf("Couldn't set up worktree for %s: %v", assignment.Ticket, err),
			role)
		_ = orch.checkIns.SetIdle(ctx, role)
		return
	}
	defer workSession.Cleanup(ctx)

	orch.writeMCPConfig(agentInstance, workSession.Dir())
	defer func() { _ = agent.RemoveMCPConfig(workSession.Dir()) }()

	prompt := BuildImplementationPrompt(assignment.Ticket, assignment.Description)

	result, err := agentInstance.ExecuteTask(ctx, prompt, nil, workSession.Dir())
	if err != nil {
		log.Printf("session error for %s on %s: %v", role, assignment.Ticket, err)
		orch.recordSessionEnd(role, assignment.Ticket, false)
		orch.postAsRole(ctx, "engineering",
			fmt.Sprintf("Hit an issue with %s — will need to pick this up again", assignment.Ticket),
			role)
		_ = orch.checkIns.SetIdle(ctx, role)
		return
	}

	orch.recordSessionEnd(role, assignment.Ticket, true)

	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Finished %s. PR should be up for review.", assignment.Ticket),
		role)

	orch.postAsRole(ctx, "reviews",
		fmt.Sprintf("%s completed work on %s — please review", role, assignment.Ticket),
		role)

	pmAgent := orch.agents[agent.RolePM]
	if pmAgent != nil {
		go FlushSessionMemory(ctx, pmAgent, agentInstance, assignment.Ticket, result.Transcript)
	}

	prURL := ExtractPRURL(result.Transcript)
	if prURL != "" {
		go MoveTicketState(ctx, orch.agents[agent.RolePM], assignment.Ticket, "In Review")
		orch.startReview(ctx, prURL, assignment.Ticket)
	}

	_ = orch.checkIns.SetIdle(ctx, role)
}

func (orch *Orchestrator) postAsRole(ctx context.Context, channel, text string, role agent.Role) {
	if orch.bot == nil {
		return
	}
	_ = orch.bot.PostAsRole(ctx, channel, text, role)

	if orch.conversation != nil {
		go orch.conversation.OnMessage(ctx, channel, string(role), text)
	}
}

// cancelSession cancels a running session for the given role, if any.
func (orch *Orchestrator) cancelSession(role agent.Role) {
	orch.cancelsMu.Lock()
	defer orch.cancelsMu.Unlock()

	cancel, ok := orch.sessionCancels[role]
	if !ok {
		return
	}
	cancel()
	delete(orch.sessionCancels, role)
}

// cancelAllSessions cancels every running session.
func (orch *Orchestrator) cancelAllSessions() {
	orch.cancelsMu.Lock()
	defer orch.cancelsMu.Unlock()

	for role, cancel := range orch.sessionCancels {
		cancel()
		delete(orch.sessionCancels, role)
	}
}

func (orch *Orchestrator) registerSessionCancel(role agent.Role, cancel context.CancelFunc) {
	orch.cancelsMu.Lock()
	defer orch.cancelsMu.Unlock()
	orch.sessionCancels[role] = cancel
}

func (orch *Orchestrator) clearSessionCancel(role agent.Role) {
	orch.cancelsMu.Lock()
	defer orch.cancelsMu.Unlock()
	delete(orch.sessionCancels, role)
}

// filterHealthyEngineers returns engineers that are not in a failing
// health state.
func (orch *Orchestrator) filterHealthyEngineers(roles []agent.Role) []agent.Role {
	engineers := filterEngineers(roles)

	if orch.monitor == nil {
		return engineers
	}

	healthy := make([]agent.Role, 0, len(engineers))
	for _, role := range engineers {
		agentHealth, err := orch.monitor.GetHealth(role)
		if err != nil {
			healthy = append(healthy, role)
			continue
		}
		if agentHealth.State == health.StateFailing {
			log.Printf("tick: skipping %s — health state is failing", role)
			continue
		}
		healthy = append(healthy, role)
	}

	return healthy
}

func (orch *Orchestrator) recordSessionStart(role agent.Role) {
	if orch.monitor == nil {
		return
	}
	orch.monitor.RecordSessionStart(role)
}

func (orch *Orchestrator) recordSessionEnd(role agent.Role, ticket string, success bool) {
	if orch.monitor == nil {
		return
	}
	orch.monitor.RecordSessionEnd(role, ticket, success)
}

// writeMCPConfig writes the .mcp.json to the session working directory
// and sets the agent's MCPConfigPath so the Claude Code process can
// find it.
func (orch *Orchestrator) writeMCPConfig(agentInstance *agent.Agent, workDir string) {
	mcpCfg := agent.BuildMCPConfig(agent.MCPOptions{
		MemoryBinaryPath: orch.cfg.MemoryBinaryPath,
		AgentDBPath:      agentInstance.DBPath(),
	})

	if err := agent.WriteMCPConfig(workDir, mcpCfg); err != nil {
		log.Printf("failed to write MCP config for %s: %v", agentInstance.Role(), err)
		return
	}

	agentInstance.MCPConfigPath = filepath.Join(workDir, ".mcp.json")
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
