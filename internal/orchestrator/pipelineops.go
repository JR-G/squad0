package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
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

	orch.announceAsRole(ctx, "triage",
		fmt.Sprintf("%s has had %d review cycles — needs human attention", ticket, item.ReviewCycles),
		agent.RolePM)

	return true
}

// resumePendingWork checks the pipeline for non-terminal work items
// from a previous run and resumes them. Called on startup.
func (orch *Orchestrator) resumePendingWork(ctx context.Context) {
	if orch.pipelineStore == nil {
		return
	}

	for role := range orch.agents {
		openItems, err := orch.pipelineStore.OpenByEngineer(ctx, role)
		if err != nil {
			continue
		}

		for _, item := range openItems {
			orch.resumeWorkItem(ctx, item)
		}
	}
}

func (orch *Orchestrator) resumeWorkItem(ctx context.Context, item pipeline.WorkItem) {
	log.Printf("resuming %s (stage: %s, PR: %s)", item.Ticket, item.Stage, item.PRURL)

	// If the item has a PR, check the actual GitHub state and fast-forward
	// the pipeline. The local stage may be stale from a previous restart.
	if item.PRURL != "" {
		orch.resumeWithGitHubState(ctx, item)
		return
	}

	switch item.Stage { //nolint:exhaustive // only actionable stages handled
	case pipeline.StageWorking:
		orch.resumeStaleWorkingItem(ctx, item)
	case pipeline.StageAssigned:
		log.Printf("work item %s was assigned but not started — will be re-assigned", item.Ticket)
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

// resumeStaleWorkingItem handles a StageWorking item with no PR.
// If it has been working for more than 30 minutes, the work is stale
// and needs reassignment. Otherwise it is left for the engineer to
// pick up naturally.
func (orch *Orchestrator) resumeStaleWorkingItem(ctx context.Context, item pipeline.WorkItem) {
	age := time.Since(item.UpdatedAt)
	if age <= staleWorkThreshold {
		log.Printf("work item %s is only %s old — engineer will re-pick it up", item.Ticket, age.Round(time.Minute))
		return
	}

	log.Printf("work item %s is stale (%s) — marking failed for reassignment", item.Ticket, age.Round(time.Minute))

	orch.advancePipeline(ctx, item.ID, pipeline.StageFailed)

	name := orch.NameForRole(item.Engineer)
	ticketLink := orch.cfg.Links.TicketLink(item.Ticket)
	orch.announceAsRole(ctx, "engineering",
		fmt.Sprintf("%s's work on %s stalled after %s with no PR — returning it to the backlog.",
			name, ticketLink, formatDuration(age)),
		agent.RolePM)
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
		// Resume it instead of blocking new work forever.
		if orch.isRoleIdle(ctx, role) {
			log.Printf("tick: %s is idle but has stale work — resuming", role)
			for _, item := range openItems {
				orch.resumeWorkItem(ctx, item)
			}
			continue
		}

		log.Printf("tick: skipping %s — has %d open work items", role, len(openItems))
	}

	return available
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
