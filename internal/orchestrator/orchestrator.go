package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/pipeline"
	"github.com/JR-G/squad0/internal/routing"
)

// Orchestrator coordinates the squad0 agent team: polls for work via
// the PM, assigns tickets to idle engineers, runs sessions, and
// captures results.
type Orchestrator struct {
	cfg          Config
	agents       map[agent.Role]*agent.Agent
	mailboxes    map[agent.Role]*agent.Mailbox
	checkIns     *coordination.CheckInStore
	bot          *slack.Bot
	assigner     *Assigner
	scheduler    *WorkScheduler
	health       *HealthSupervisor
	chat         *ConversationCoordinator
	running      bool
	conversation *ConversationEngine
	sessions     *SessionTracker

	roster               map[agent.Role]string
	pipelineStore        *pipeline.WorkItemStore
	handoffStore         *pipeline.HandoffStore
	projectEpisodeStore  *memory.EpisodeStore
	projectFactStore     *memory.FactStore
	followedUp           map[int64]bool
	mergeAnnounced       map[string]bool
	mergeAnnounceMu      sync.Mutex
	concerns             *ConcernTracker
	eventBus             *EventBus
	situations           *SituationQueue
	escalations          *EscalationTracker
	specStore            *routing.SpecialisationStore
	opinionStore         *routing.OpinionStore
	tokenLedger          *routing.TokenLedger
	complexityClassifier *routing.ComplexityClassifier
	startedAt            time.Time
}

// Config holds orchestrator-level settings.
type Config struct {
	PollInterval      time.Duration
	MaxParallel       int
	CooldownAfter     time.Duration
	WorkEnabled       bool
	TargetRepoDir     string
	MemoryBinaryPath  string
	Links             slack.LinkConfig
	DiscussionWait    time.Duration
	QuietThreshold    time.Duration
	QuietPollInterval time.Duration
	AcknowledgePause  time.Duration
}

// NewOrchestrator creates an Orchestrator with all dependencies injected.
func NewOrchestrator(
	cfg Config,
	agents map[agent.Role]*agent.Agent,
	checkIns *coordination.CheckInStore,
	bot *slack.Bot,
	assigner *Assigner,
) *Orchestrator {
	mailboxes := make(map[agent.Role]*agent.Mailbox, len(agents))
	for role, instance := range agents {
		mailbox := agent.NewMailbox(instance, mailboxCapacity)
		mailbox.Start(context.Background())
		mailboxes[role] = mailbox
	}

	return &Orchestrator{
		cfg:            cfg,
		agents:         agents,
		mailboxes:      mailboxes,
		checkIns:       checkIns,
		bot:            bot,
		assigner:       assigner,
		scheduler:      NewWorkScheduler(assigner),
		sessions:       NewSessionTracker(),
		health:         NewHealthSupervisor(nil),
		chat:           NewConversationCoordinator(bot),
		followedUp:     make(map[int64]bool),
		mergeAnnounced: make(map[string]bool),
	}
}

const mailboxCapacity = 16

// SetHealthMonitor connects the health monitor.
func (orch *Orchestrator) SetHealthMonitor(monitor *health.Monitor) {
	orch.health = NewHealthSupervisor(monitor)
}

// Run starts the main orchestration loop. Blocks until ctx is cancelled
// and any in-flight session goroutines drain (bounded by shutdownGrace).
func (orch *Orchestrator) Run(ctx context.Context) error {
	orch.running = true
	defer func() { orch.running = false }()

	defer orch.stopMailboxes()

	if err := orch.initialiseCheckIns(ctx); err != nil {
		return fmt.Errorf("initialising check-ins: %w", err)
	}

	orch.startedAt = time.Now()
	log.Println("orchestrator started")
	orch.announceAsRole(ctx, "feed", "Squad0 is online. Ready to work.", agent.RolePM)
	orch.resumePendingWork(ctx)
	orch.recoverOrphanedPRs(ctx)

	ticker := time.NewTicker(orch.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("orchestrator stopping — draining in-flight sessions")
			orch.drainSessions()
			return ctx.Err()
		case <-ticker.C:
			orch.tick(ctx)
		}
	}
}

func (orch *Orchestrator) stopMailboxes() {
	for _, mailbox := range orch.mailboxes {
		mailbox.Stop()
	}
}

// MailboxFor returns the mailbox wrapping the given agent role,
// or nil if no mailbox exists. Returned mailbox is already started
// when Run is in progress.
func (orch *Orchestrator) MailboxFor(role agent.Role) *agent.Mailbox {
	return orch.mailboxes[role]
}

// executeViaMailbox routes ExecuteTask through the per-agent mailbox
// when one is available, falling back to a direct call otherwise so
// tests that build orchestrators without Run() (and therefore without
// started mailboxes) still work.
func (orch *Orchestrator) executeViaMailbox(ctx context.Context, role agent.Role, prompt, workDir string) (agent.SessionResult, error) {
	mailbox := orch.mailboxes[role]
	if mailbox == nil {
		return orch.agents[role].ExecuteTask(ctx, prompt, nil, workDir)
	}
	return mailbox.Execute(prompt, nil, workDir)
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

	orch.breakSilence(ctx)

	if !orch.cfg.WorkEnabled {
		return
	}
	orch.RunSensors(ctx)
	orch.RunWitnessScan(ctx)
	orch.RunPMDuties(ctx)

	idleRoles, err := orch.checkIns.IdleAgents(ctx)
	if err != nil {
		log.Printf("error checking idle agents: %v", err)
		return
	}

	log.Printf("tick: %d idle agents, roles: %v", len(idleRoles), idleRoles)

	// Priority 1: Fix conflicting PRs — nothing else matters until conflicts are resolved.
	orch.resolveConflicts(ctx, idleRoles)

	// Priority 2: Review unreviewed PRs before assigning new work.
	orch.triggerPendingReviews(ctx, idleRoles)

	// Priority 3: Assign new work to free engineers.
	idleEngineers := orch.filterByWIP(ctx, orch.filterHealthyEngineers(idleRoles))
	orch.tryAssignWork(ctx, idleEngineers)

	// Idle duties for non-engineers (Designer, Tech Lead) + unassigned engineers.
	orch.RunIdleDuties(ctx, filterIdleDutyRoles(idleRoles))
}

// tryAssignWork delegates the in-flight gate + Linear fetch to the
// WorkScheduler, then dispatches each match by spawning a session
// and emitting idle events for engineers that didn't get a ticket.
// Dispatch stays on the orchestrator because it touches the agent
// map, pipeline, and check-in store — none of which the scheduler
// should know about.
func (orch *Orchestrator) tryAssignWork(ctx context.Context, idleEngineers []agent.Role) {
	if len(idleEngineers) == 0 {
		log.Println("tick: no idle engineers")
		return
	}

	log.Printf("tick: requesting assignments for %v", idleEngineers)

	if !orch.scheduler.Schedule(ctx, idleEngineers, orch.dispatchAssignments) {
		log.Println("tick: assignment already in progress, skipping")
	}
}

func (orch *Orchestrator) dispatchAssignments(ctx context.Context, assignments []Assignment, eligible []agent.Role) {
	log.Printf("tick: got %d assignments", len(assignments))

	if len(assignments) == 0 {
		// No tickets available — engage idle engineers with PR reviews.
		for _, role := range eligible {
			orch.emitEvent(ctx, EventAgentIdle, "", "", 0, role)
		}
		orch.RunIdleDuties(ctx, eligible)
		return
	}

	assignedSet := make(map[agent.Role]bool, len(assignments))
	for _, assignment := range assignments {
		log.Printf("tick: assigning %s to %s", assignment.Ticket, assignment.Role)
		orch.startWork(ctx, assignment)
		assignedSet[assignment.Role] = true
	}

	unassigned := make([]agent.Role, 0, len(eligible))
	for _, role := range eligible {
		if !assignedSet[role] {
			unassigned = append(unassigned, role)
			orch.emitEvent(ctx, EventAgentIdle, "", "", 0, role)
		}
	}
	orch.RunIdleDuties(ctx, unassigned)
}

// SetConversationEngine connects the conversation engine to the orchestrator
// and wires up the pause checker so paused agents stay silent.
func (orch *Orchestrator) SetConversationEngine(engine *ConversationEngine) {
	orch.conversation = engine
	orch.chat.SetConversation(engine)
	engine.SetPauseChecker(orch.IsPaused)
}

// SetPipeline connects the work item pipeline for WIP tracking.
func (orch *Orchestrator) SetPipeline(store *pipeline.WorkItemStore) {
	orch.pipelineStore = store
}

// SetProjectEpisodeStore connects the shared episode store for seance
// — cross-session memory retrieval when tickets are reassigned.
func (orch *Orchestrator) SetProjectEpisodeStore(store *memory.EpisodeStore) {
	orch.projectEpisodeStore = store
}

// SetProjectFactStore connects the shared fact store for
// cross-pollination and seance.
func (orch *Orchestrator) SetProjectFactStore(store *memory.FactStore) {
	orch.projectFactStore = store
}

// SetRoster stores the role→name mapping so lifecycle messages use
// chosen names instead of role IDs.
func (orch *Orchestrator) SetRoster(roster map[agent.Role]string) {
	orch.roster = roster
	orch.chat.SetRoster(roster)
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

	assignment.WorkItemID = orch.createPipelineItem(ctx, assignment)
	go MoveTicketState(ctx, orch.agents[agent.RolePM], assignment.Ticket, "In Progress")

	orch.sessions.Add(1)
	go func() {
		defer orch.sessions.Done()
		defer orch.clearSessionCancel(assignment.Role)
		orch.runSession(sessionCtx, agentInstance, assignment)
	}()
}

// Wait blocks until all running sessions complete. Unbounded —
// shutdown uses orch.sessions.DrainFor with a deadline instead.
// Public for tests that need to drain before assertions.
func (orch *Orchestrator) Wait() {
	orch.sessions.Wait()
}

func (orch *Orchestrator) runSession(ctx context.Context, agentInstance *agent.Agent, assignment Assignment) {
	role := agentInstance.Role()

	orch.recordSessionStart(role)
	ticketLink := orch.cfg.Links.TicketLink(assignment.Ticket)

	// Discussion phase — engineer posts plan, team responds. The
	// discussion returns the raw transcript plus any DECISION lines
	// extracted from it so they can flow into the implementation
	// prompt as binding commitments.
	discussion, decisions := orch.runDiscussionPhase(ctx, agentInstance, assignment)
	assignment.Decisions = decisions

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

	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Starting work on %s — heads down, will update when I have a PR.", ticketLink), role)
	time.Sleep(orch.acknowledgePause())
	orch.acknowledgeThread(ctx, agentInstance, role, "engineering")

	seanceCtx := BuildSeanceContextFull(ctx, orch.projectEpisodeStore, orch.agentFactStores(), orch.handoffStore, assignment.Ticket, role)
	prompt := seanceCtx + discussion + FormatDecisionsForPrompt(decisions) + BuildImplementationPrompt(assignment.Ticket, assignment.Description)
	branch := fmt.Sprintf("feat/%s", assignment.Ticket)

	agentInstance.SetCurrentSession(assignment.Ticket)
	defer agentInstance.SetCurrentSession("")

	result, err := orch.executeViaMailbox(ctx, role, prompt, workSession.Dir())
	if err != nil {
		log.Printf("session error for %s on %s: %v", role, assignment.Ticket, err)
		orch.recordSessionEnd(role, assignment.Ticket, false)
		orch.checkCircuitBreaker(ctx, assignment.Ticket)
		orch.writeHandoff(ctx, assignment.Ticket, role, "failed", result.Transcript, branch)
		orch.postAsRole(ctx, "engineering",
			fmt.Sprintf("Hit an issue with %s — will need to pick this up again", assignment.Ticket),
			role)
		_ = orch.checkIns.SetIdle(ctx, role)
		orch.emitEvent(ctx, EventSessionFailed, "", assignment.Ticket, assignment.WorkItemID, role)
		return
	}

	orch.recordSessionEnd(role, assignment.Ticket, true)

	prURL := ExtractPRURL(result.Transcript)

	handoffStatus := "completed"
	if prURL == "" {
		handoffStatus = "partial"
	}
	orch.writeHandoff(ctx, assignment.Ticket, role, handoffStatus, result.Transcript, branch)

	// Pre-submission checklist — verify work is clean before review.
	RunPreSubmitCheck(ctx, agentInstance, workSession.Dir())

	// If the session produced no PR, try once more with a targeted
	// DirectSession before giving up. Engineers sometimes complete
	// the work but skip the push/PR step.
	if prURL == "" {
		prURL = orch.rescuePR(ctx, agentInstance, workSession.Dir(), assignment.Ticket, branch)
	}

	orch.announceSessionResult(ctx, prURL, ticketLink, assignment.WorkItemID, role)

	orch.storeProjectEpisode(ctx, role, assignment.Ticket, result.Transcript)
	orch.runPostSessionAsync(ctx, agentInstance, assignment.Ticket, result.Transcript)

	if prURL != "" {
		go MoveTicketState(ctx, orch.agents[agent.RolePM], assignment.Ticket, "In Review")
		// Don't set idle — the review/merge lifecycle owns the engineer's
		// state until the PR is merged, failed, or escalated.
		orch.startReview(ctx, prURL, assignment.Ticket, assignment.WorkItemID, role)
		orch.emitEvent(ctx, EventSessionComplete, prURL, assignment.Ticket, assignment.WorkItemID, role)
		return
	}
	_ = orch.checkIns.SetIdle(ctx, role)
	orch.emitEvent(ctx, EventSessionComplete, "", assignment.Ticket, assignment.WorkItemID, role)
}

func (orch *Orchestrator) postAsRole(ctx context.Context, channel, text string, role agent.Role) {
	orch.chat.Post(ctx, channel, text, role)
}

// AnnounceForTest exports announceAsRole for testing.
func (orch *Orchestrator) AnnounceForTest(ctx context.Context, channel, text string, role agent.Role) {
	orch.announceAsRole(ctx, channel, text, role)
}

// announceAsRole posts a message without triggering the conversation
// engine. Used for status updates and announcements that don't need
// agent responses.
func (orch *Orchestrator) announceAsRole(ctx context.Context, channel, text string, role agent.Role) {
	orch.chat.Announce(ctx, channel, text, role)
}

// RegisterCancelForTest exports registerSessionCancel for testing.
func (orch *Orchestrator) RegisterCancelForTest(role agent.Role, cancel context.CancelFunc) {
	orch.registerSessionCancel(role, cancel)
}

// cancelSession, cancelAllSessions, registerSessionCancel,
// clearSessionCancel are thin forwards to the SessionTracker. Kept
// as orchestrator-level helpers so existing callers don't need to
// reach into orch.sessions directly — the tracker is an internal
// implementation detail. Test-export wrappers stay below.
func (orch *Orchestrator) cancelSession(role agent.Role) { orch.sessions.Cancel(role) }
func (orch *Orchestrator) cancelAllSessions()            { orch.sessions.CancelAll() }
func (orch *Orchestrator) registerSessionCancel(role agent.Role, cancel context.CancelFunc) {
	orch.sessions.Register(role, cancel)
}
func (orch *Orchestrator) clearSessionCancel(role agent.Role) { orch.sessions.Clear(role) }

// filterHealthyEngineers returns engineers that are not in a failing
// health state. Thin forward to HealthSupervisor for callers that
// already have an Orchestrator handle.
func (orch *Orchestrator) filterHealthyEngineers(roles []agent.Role) []agent.Role {
	return orch.health.FilterHealthyEngineers(roles)
}
