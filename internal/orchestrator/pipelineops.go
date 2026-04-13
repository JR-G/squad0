package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/pipeline"
)

// ResumeWorkItemForTest exports resumeWorkItem for testing.
func (orch *Orchestrator) ResumeWorkItemForTest(ctx context.Context, item pipeline.WorkItem) {
	orch.resumeWorkItem(ctx, item)
}

// SetPipelinePRForTest exports setPipelinePR for testing.
func (orch *Orchestrator) SetPipelinePRForTest(ctx context.Context, itemID int64, prURL string) {
	orch.setPipelinePR(ctx, itemID, prURL)
}

// StoreProjectEpisodeForTest exports storeProjectEpisode for testing.
func (orch *Orchestrator) StoreProjectEpisodeForTest(ctx context.Context, role agent.Role, ticket, transcript string) {
	orch.storeProjectEpisode(ctx, role, ticket, transcript)
}

// CreatePipelineItemForTest exports createPipelineItem for testing.
func (orch *Orchestrator) CreatePipelineItemForTest(ctx context.Context, assignment Assignment) int64 {
	return orch.createPipelineItem(ctx, assignment)
}

func (orch *Orchestrator) setPipelinePR(ctx context.Context, itemID int64, prURL string) {
	if orch.pipelineStore == nil || itemID == 0 {
		return
	}

	_ = orch.pipelineStore.SetPRURL(ctx, itemID, prURL)
	orch.advancePipeline(ctx, itemID, pipeline.StagePROpened)
}

func (orch *Orchestrator) storeProjectEpisode(ctx context.Context, role agent.Role, ticket, transcript string) {
	if orch.projectEpisodeStore == nil {
		return
	}

	_, _ = orch.projectEpisodeStore.CreateEpisode(ctx, memory.Episode{
		Agent:   string(role),
		Ticket:  ticket,
		Summary: agent.TruncateSummary(transcript, 500),
		Outcome: memory.OutcomeSuccess,
	})
}

func (orch *Orchestrator) createPipelineItem(ctx context.Context, assignment Assignment) int64 {
	if orch.pipelineStore == nil {
		return 0
	}

	branch := fmt.Sprintf("feat/%s", strings.ToLower(assignment.Ticket))
	itemID, err := orch.pipelineStore.Create(ctx, pipeline.WorkItem{
		Ticket:   assignment.Ticket,
		Engineer: assignment.Role,
		Stage:    pipeline.StageWorking,
		Branch:   branch,
	})
	if err != nil {
		log.Printf("failed to create pipeline item for %s: %v", assignment.Ticket, err)
		return 0
	}

	return itemID
}

func (orch *Orchestrator) advancePipeline(ctx context.Context, itemID int64, stage pipeline.Stage) {
	if orch.pipelineStore == nil || itemID == 0 {
		return
	}

	if err := orch.pipelineStore.Advance(ctx, itemID, stage); err != nil {
		log.Printf("failed to advance pipeline item %d to %s: %v", itemID, stage, err)
	}
}

func (orch *Orchestrator) shouldEscalate(ctx context.Context, workItemID int64, ticket string) bool {
	if orch.pipelineStore == nil || workItemID == 0 {
		return false
	}

	_ = orch.pipelineStore.IncrementReviewCycles(ctx, workItemID)

	item, err := orch.pipelineStore.GetByID(ctx, workItemID)
	if err != nil {
		return false
	}

	if item.ReviewCycles < maxReviewCycles {
		return false
	}

	// After max cycles, block the ticket and surface to triage.
	// Never force-approve — buggy code must not be merged. The PM
	// can reassign to a different engineer for a fresh perspective.
	orch.announceAsRole(ctx, "reviews",
		fmt.Sprintf("%s has had %d review cycles without resolution — blocking for reassignment.", ticket, item.ReviewCycles),
		agent.RolePM)
	orch.announceAsRole(ctx, "triage",
		fmt.Sprintf("%s blocked after %d review cycles — needs reassignment or human review", ticket, item.ReviewCycles),
		agent.RolePM)
	orch.advancePipeline(ctx, workItemID, pipeline.StageFailed)

	pmAgent := orch.agents[agent.RolePM]
	if pmAgent != nil {
		go MoveTicketState(ctx, pmAgent, ticket, "Todo")
	}

	return true
}

// resumePendingWork checks the pipeline for non-terminal work items
// from a previous run and resumes them. Called on startup.
//
// squad0's engineer sessions are child processes that die when the
// binary restarts, so on boot any StageWorking-with-no-PR item is by
// definition a dead session and must be respawned — otherwise the
// engineer sits idle with "has open work" but nothing ever starts it.
// StageAssigned items are in the same boat: the assigner picked them
// last time but the restart killed the spawn before it ran.
func (orch *Orchestrator) resumePendingWork(ctx context.Context) {
	if orch.pipelineStore == nil {
		return
	}

	for role := range orch.agents {
		openItems, err := orch.pipelineStore.OpenByEngineer(ctx, role)
		if err != nil {
			continue
		}

		// Resume only the most recent item per role — don't launch
		// all reviews simultaneously on startup.
		if len(openItems) > 0 {
			orch.resumeWorkItem(ctx, openItems[0])
		}
	}
}

// resumeAssignment respawns an engineer session for an existing
// pipeline item. Used for crash recovery: the pipeline row already
// exists, so we reuse its ID rather than creating a new one. Safe to
// call for items in StageAssigned (never got to StageWorking) and
// StageWorking-with-no-PR (session died before opening a PR).
func (orch *Orchestrator) resumeAssignment(ctx context.Context, item pipeline.WorkItem) {
	agentInstance, ok := orch.agents[item.Engineer]
	if !ok {
		log.Printf("resume: no agent for role %s on %s", item.Engineer, item.Ticket)
		return
	}

	if err := orch.checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         item.Engineer,
		Ticket:        item.Ticket,
		Status:        coordination.StatusWorking,
		FilesTouching: []string{},
		Message:       fmt.Sprintf("resuming %s", item.Ticket),
	}); err != nil {
		log.Printf("resume: checkin upsert failed for %s: %v", item.Engineer, err)
		return
	}

	if item.Stage != pipeline.StageWorking {
		orch.advancePipeline(ctx, item.ID, pipeline.StageWorking)
	}

	assignment := Assignment{
		Role:       item.Engineer,
		Ticket:     item.Ticket,
		WorkItemID: item.ID,
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	orch.registerSessionCancel(item.Engineer, cancel)

	orch.wg.Add(1)
	go func() {
		defer orch.wg.Done()
		defer orch.clearSessionCancel(item.Engineer)
		orch.runSession(sessionCtx, agentInstance, assignment)
	}()

	log.Printf("resume: respawned session for %s on %s (item %d)", item.Engineer, item.Ticket, item.ID)
}

// staleWorkingThreshold is how long a StageWorking item with no PR
// can linger before we give up and mark it failed. Items that
// haven't produced a PR in this long have usually hit a real problem
// that respawning won't fix.
const staleWorkingThreshold = 30 * time.Minute

func (orch *Orchestrator) resumeWorkItem(ctx context.Context, item pipeline.WorkItem) {
	log.Printf("resuming %s (stage: %s, PR: %s)", item.Ticket, item.Stage, item.PRURL)

	// If the item has a PR, check the actual GitHub state and fast-forward
	// the pipeline. The local stage may be stale from a previous restart.
	if item.PRURL != "" {
		orch.resumeWithGitHubState(ctx, item)
		return
	}

	if item.Stage == pipeline.StageWorking && time.Since(item.UpdatedAt) > staleWorkingThreshold {
		log.Printf("work item %s has no PR after %s — marking failed", item.Ticket, formatDuration(time.Since(item.UpdatedAt)))
		orch.failAndRequeue(ctx, item)
		return
	}

	switch item.Stage { //nolint:exhaustive // only actionable stages handled
	case pipeline.StageWorking, pipeline.StageAssigned:
		orch.resumeAssignment(ctx, item)
	}
}

// resumeWithGitHubState checks the actual PR state on GitHub before
// deciding what to do. Prevents re-review loops on restart when the
// pipeline stage is stale.
func (orch *Orchestrator) resumeWithGitHubState(ctx context.Context, item pipeline.WorkItem) {
	pmAgent := orch.agents[agent.RolePM]
	if pmAgent == nil {
		return
	}

	// Check if the PR is already merged on GitHub.
	if orch.verifyMerged(ctx, pmAgent, item.PRURL) {
		log.Printf("resume: %s is already merged on GitHub — advancing pipeline", item.Ticket)
		orch.advancePipeline(ctx, item.ID, pipeline.StageMerged)
		go MoveTicketState(ctx, pmAgent, item.Ticket, "Done")
		return
	}

	// Check the review decision on GitHub.
	status := orch.checkApprovalStatus(ctx, pmAgent, item.PRURL)

	switch status {
	case approvalStatusApproved:
		if HasOutstandingReviewComments(ctx, orch.cfg.TargetRepoDir, item.PRURL) {
			log.Printf("resume: %s is approved but has unaddressed review comments — reverting to reviewing", item.Ticket)
			orch.advancePipeline(ctx, item.ID, pipeline.StageReviewing)
			orch.startReview(ctx, item.PRURL, item.Ticket, item.ID, item.Engineer)
			return
		}
		log.Printf("resume: %s is approved on GitHub — sending engineer to merge", item.Ticket)
		orch.advancePipeline(ctx, item.ID, pipeline.StageApproved)
		orch.wg.Add(1)
		go func() {
			defer orch.wg.Done()
			orch.startEngineerMerge(ctx, item.PRURL, item.Ticket, item.ID, item.Engineer)
		}()

	case approvalStatusNotApproved:
		log.Printf("resume: %s needs review or changes on GitHub — starting review", item.Ticket)
		orch.startReview(ctx, item.PRURL, item.Ticket, item.ID, item.Engineer)

	default:
		// Error checking status — fall back to starting a review.
		log.Printf("resume: couldn't check %s status — starting review", item.Ticket)
		orch.startReview(ctx, item.PRURL, item.Ticket, item.ID, item.Engineer)
	}
}

func (orch *Orchestrator) filterByWIP(ctx context.Context, roles []agent.Role) []agent.Role {
	if orch.pipelineStore == nil {
		return roles
	}

	available := make([]agent.Role, 0, len(roles))
	for _, role := range roles {
		openItems, err := orch.pipelineStore.OpenByEngineer(ctx, role)
		if err != nil {
			available = append(available, role)
			continue
		}

		if len(openItems) == 0 {
			available = append(available, role)
			continue
		}

		// Engineer has open items but is idle — the item is stuck.
		// Items with a PR get resumed (review/merge). Items without
		// a PR are failed immediately — the engineer is idle so
		// they're clearly not working on it. No age check needed.
		if !orch.isRoleIdle(ctx, role) {
			log.Printf("tick: skipping %s — has %d open work items", role, len(openItems))
			continue
		}

		if orch.clearStaleWork(ctx, role, openItems) {
			available = append(available, role)
		}
	}

	return available
}

// clearStaleWork handles open items for an idle engineer. Returns true
// if all items were cleared and the engineer is free for new work.
//
// A StageWorking item with no PR and an idle check-in means the
// session died without progressing. Normally that's handled by
// resumePendingWork at startup, but if a mid-run session crashes we
// also respawn it here after a short grace window so the engineer
// doesn't sit blocked until the 30-minute stale timer fires.
func (orch *Orchestrator) clearStaleWork(ctx context.Context, role agent.Role, items []pipeline.WorkItem) bool {
	log.Printf("tick: %s is idle but has open work — clearing", role)
	allCleared := true

	for _, item := range items {
		if isDeadWorkingSession(item) {
			orch.handleDeadSession(ctx, role, item)
			allCleared = false
			continue
		}
		orch.resumeWorkItem(ctx, item)
		allCleared = false
	}

	return allCleared
}

func isDeadWorkingSession(item pipeline.WorkItem) bool {
	return item.Stage == pipeline.StageWorking && item.PRURL == ""
}

func (orch *Orchestrator) handleDeadSession(ctx context.Context, role agent.Role, item pipeline.WorkItem) {
	if time.Since(item.UpdatedAt) <= runtimeSessionDeadGrace {
		log.Printf("tick: %s has working item %s with no PR — waiting for session", role, item.Ticket)
		return
	}
	log.Printf("tick: %s has dead session on %s — respawning", role, item.Ticket)
	orch.resumeAssignment(ctx, item)
}

// runtimeSessionDeadGrace is how long a StageWorking item without a
// PR is left alone before the tick loop concludes the session died
// and respawns it. Long enough to avoid racing a legitimate session
// mid-transition, short enough to recover from crashes quickly.
const runtimeSessionDeadGrace = 90 * time.Second

// CheckCircuitBreakerForTest exports checkCircuitBreaker for testing.
func (orch *Orchestrator) CheckCircuitBreakerForTest(ctx context.Context, ticket string) {
	orch.checkCircuitBreaker(ctx, ticket)
}

func (orch *Orchestrator) checkCircuitBreaker(ctx context.Context, ticket string) {
	if orch.assigner == nil {
		return
	}

	if !orch.assigner.RecordAssignmentFailure(ticket) {
		return
	}

	log.Printf("circuit breaker open for %s — 3+ failures", ticket)
	orch.announceAsRole(ctx, "triage",
		fmt.Sprintf("%s has failed 3+ times — needs investigation", ticket),
		agent.RolePM)
}

// FailAndRequeueForTest exports failAndRequeue for testing.
func (orch *Orchestrator) FailAndRequeueForTest(ctx context.Context, item pipeline.WorkItem) {
	orch.failAndRequeue(ctx, item)
}

// failAndRequeue marks a pipeline item as failed and moves the Linear
// ticket back to Todo so it can be reassigned.
func (orch *Orchestrator) failAndRequeue(ctx context.Context, item pipeline.WorkItem) {
	orch.advancePipeline(ctx, item.ID, pipeline.StageFailed)
	pmAgent := orch.agents[agent.RolePM]
	if pmAgent != nil {
		go MoveTicketState(ctx, pmAgent, item.Ticket, "Todo")
	}
}

// AnnounceSessionResultForTest exports announceSessionResult for testing.
func (orch *Orchestrator) AnnounceSessionResultForTest(ctx context.Context, prURL, ticketLink string, workItemID int64, role agent.Role) {
	orch.announceSessionResult(ctx, prURL, ticketLink, workItemID, role)
}

// announceSessionResult handles the post-session PR state: sets the
// pipeline PR, posts announcements, or marks the item failed if no PR.
func (orch *Orchestrator) announceSessionResult(ctx context.Context, prURL, ticketLink string, workItemID int64, role agent.Role) {
	if prURL == "" {
		orch.advancePipeline(ctx, workItemID, pipeline.StageFailed)
		orch.announceAsRole(ctx, "engineering",
			fmt.Sprintf("Finished work on %s but couldn't open a PR — will need another pass.", ticketLink), role)
		return
	}

	orch.setPipelinePR(ctx, workItemID, prURL)
	prLink := orch.cfg.Links.PRLink(prURL)
	orch.announceAsRole(ctx, "engineering",
		fmt.Sprintf("Finished %s — %s", ticketLink, prLink), role)
	orch.announceAsRole(ctx, "reviews",
		fmt.Sprintf("Finished %s — %s", ticketLink, prLink), role)
}

func (orch *Orchestrator) isRoleIdle(ctx context.Context, role agent.Role) bool {
	checkIn, err := orch.checkIns.GetByAgent(ctx, role)
	if err != nil {
		return true // No check-in row means idle.
	}
	return checkIn.Status == "idle"
}

// SetIdleIfStillWorkingForTest exports setIdleIfStillWorking for testing.
func (orch *Orchestrator) SetIdleIfStillWorkingForTest(ctx context.Context, role agent.Role) {
	orch.setIdleIfStillWorking(ctx, role)
}

// setIdleIfStillWorking sets the engineer idle only if they're currently
// checked in as working. Prevents clobbering a status set by a later
// lifecycle step (e.g. fix-up or merge already set them to a new state).
func (orch *Orchestrator) setIdleIfStillWorking(ctx context.Context, role agent.Role) {
	checkIn, err := orch.checkIns.GetByAgent(ctx, role)
	if err != nil {
		return
	}

	if checkIn.Status != coordination.StatusWorking {
		return
	}

	log.Printf("wip: %s still working after review lifecycle ended — setting idle", role)
	_ = orch.checkIns.SetIdle(ctx, role)
}
