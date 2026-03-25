package orchestrator

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/pipeline"
)

const maxReviewCycles = 3

var prURLPattern = regexp.MustCompile(`https://github\.com/[^/]+/[^/]+/pull/\d+`)

var prRepoPattern = regexp.MustCompile(`https://github\.com/([^/]+/[^/]+)/pull/\d+`)

// ReviewOutcome classifies what the reviewer decided.
type ReviewOutcome string

const (
	// ReviewApproved means the PR passed review.
	ReviewApproved ReviewOutcome = "approved"
	// ReviewChangesRequested means the reviewer wants fixes.
	ReviewChangesRequested ReviewOutcome = "changes_requested"
)

// Approval status constants returned by checkApprovalStatus.
const (
	approvalStatusApproved    = "APPROVED"
	approvalStatusNotApproved = "NOT_APPROVED"
	approvalStatusError       = "ERROR"
)

// ExtractPRURL finds a GitHub pull request URL in the given text.
func ExtractPRURL(text string) string {
	return prURLPattern.FindString(text)
}

// ExtractPRNumber returns the PR number from a GitHub PR URL.
func ExtractPRNumber(prURL string) string {
	idx := strings.LastIndex(prURL, "/")
	if idx == -1 {
		return ""
	}
	return prURL[idx+1:]
}

// ExtractRepo returns "owner/repo" from a GitHub PR URL.
// For example, "https://github.com/JR-G/makebook/pull/11" returns "JR-G/makebook".
func ExtractRepo(prURL string) string {
	matches := prRepoPattern.FindStringSubmatch(prURL)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// ClassifyReviewOutcome scans the reviewer's transcript for approval
// or change request signals. Defaults to changes_requested to avoid
// merging unreviewed code.
func ClassifyReviewOutcome(transcript string) ReviewOutcome {
	upper := strings.ToUpper(transcript)

	approveSignals := []string{"APPROVED", "LGTM", "LOOKS GOOD"}
	changeSignals := []string{"CHANGES_REQUESTED", "REQUEST CHANGES", "NEEDS CHANGES", "PLEASE FIX"}

	for _, signal := range changeSignals {
		if strings.Contains(upper, signal) {
			return ReviewChangesRequested
		}
	}

	for _, signal := range approveSignals {
		if strings.Contains(upper, signal) {
			return ReviewApproved
		}
	}

	// Default to changes requested — don't silently approve.
	return ReviewChangesRequested
}

// StartReviewForTest is an exported wrapper for testing.
func (orch *Orchestrator) StartReviewForTest(ctx context.Context, prURL, ticket string) {
	orch.startReview(ctx, prURL, ticket, 0, "")
}

// StartReviewWithItemForTest is an exported wrapper that passes
// work item ID and engineer role for testing the feedback loop.
func (orch *Orchestrator) StartReviewWithItemForTest(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	orch.startReview(ctx, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) startReview(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		log.Printf("no reviewer agent available for PR review")
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageReviewing)

	err := orch.checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RoleReviewer,
		Ticket:        ticket,
		Status:        coordination.StatusReviewing,
		FilesTouching: []string{},
		Message:       fmt.Sprintf("reviewing PR for %s", ticket),
	})
	if err != nil {
		log.Printf("failed to update reviewer check-in: %v", err)
		return
	}

	orch.wg.Add(1)
	go func() {
		defer orch.wg.Done()
		defer func() { _ = orch.checkIns.SetIdle(ctx, agent.RoleReviewer) }()

		orch.runReview(ctx, reviewer, prURL, ticket, workItemID, engineerRole, false)
	}()
}

func (orch *Orchestrator) runReview(ctx context.Context, reviewer *agent.Agent, prURL, ticket string, workItemID int64, engineerRole agent.Role, isReReview bool) {
	log.Printf("review: starting review of %s (re-review=%v)", ticket, isReReview)

	// Narrate — team sees the reviewer is active.
	prLink := orch.cfg.Links.PRLink(prURL)
	reviewMsg := fmt.Sprintf("Picking up %s for review. %s", ticket, prLink)
	if isReReview {
		reviewMsg = fmt.Sprintf("Re-reviewing %s — checking if the feedback was addressed. %s", ticket, prLink)
	}
	orch.postAsRole(ctx, "reviews", reviewMsg, agent.RoleReviewer)

	prompt := BuildReviewPrompt(prURL, ticket)
	if isReReview {
		prompt = BuildReReviewPrompt(prURL, ticket)
	}

	result, err := reviewer.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("review session failed for %s: %v", ticket, err)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("Couldn't complete review for %s — will try again later", ticket),
			agent.RoleReviewer)
		return
	}

	outcome := ClassifyReviewOutcome(result.Transcript)
	summary := agent.TruncateSummary(result.Transcript, 300)

	switch outcome {
	case ReviewApproved:
		orch.forceApproval(ctx, reviewer, prURL, ticket)

		archOutcome := orch.RunConversationalArchReview(ctx, prURL, ticket, engineerRole)
		if archOutcome == ReviewChangesRequested {
			orch.handleChangesRequested(ctx, prURL, ticket, workItemID, engineerRole, "Tech Lead requested architectural changes")
			return
		}

		orch.advancePipeline(ctx, workItemID, pipeline.StageApproved)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("Approved %s: %s", ticket, summary),
			agent.RoleReviewer)

		orch.mergeAndComplete(ctx, prURL, ticket, workItemID, engineerRole)

	case ReviewChangesRequested:
		orch.handleChangesRequested(ctx, prURL, ticket, workItemID, engineerRole, summary)
	}
}

// MergeForTest exports mergeAndComplete for testing.
func (orch *Orchestrator) MergeForTest(ctx context.Context, prURL, ticket string, workItemID int64) {
	orch.mergeAndComplete(ctx, prURL, ticket, workItemID, "")
}

// MergeWithEngineerForTest exports mergeAndComplete with engineer role for testing.
func (orch *Orchestrator) MergeWithEngineerForTest(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	orch.mergeAndComplete(ctx, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) mergeAndComplete(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	mergeAgent := orch.agents[agent.RolePM]
	if mergeAgent == nil {
		return
	}

	// Step 1: Verify review approval — single-purpose prompt.
	approvalStatus := orch.checkApprovalStatus(ctx, mergeAgent, prURL)
	if approvalStatus == approvalStatusNotApproved {
		log.Printf("merge blocked for %s: PR not approved on GitHub, triggering re-approval", ticket)
		orch.retryApproval(ctx, prURL, ticket, workItemID, engineerRole)
		return
	}

	if approvalStatus == approvalStatusError {
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s approved but could not verify approval — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	// Step 2: Merge — single-purpose prompt.
	if !orch.executeMerge(ctx, mergeAgent, prURL, ticket, engineerRole) {
		return
	}

	// Step 3: Verify the PR is actually merged (already existed).
	if !orch.verifyMerged(ctx, mergeAgent, prURL) {
		log.Printf("merge verification failed for %s: PR not merged despite agent claiming success", ticket)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s merge was attempted but PR is still open — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageMerged)
	go MoveTicketState(ctx, mergeAgent, ticket, "Done")

	ticketLink := orch.cfg.Links.TicketLink(ticket)
	prLink := orch.cfg.Links.PRLink(prURL)
	orch.announceAsRole(ctx, "feed",
		fmt.Sprintf("Merged %s — %s", ticketLink, prLink),
		agent.RolePM)
}

// RunArchitectureReviewForTest exports runArchitectureReview for testing.
func (orch *Orchestrator) RunArchitectureReviewForTest(ctx context.Context, prURL, ticket string) ReviewOutcome {
	return orch.RunConversationalArchReview(ctx, prURL, ticket, "")
}

func (orch *Orchestrator) handleChangesRequested(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role, _ string) {
	orch.advancePipeline(ctx, workItemID, pipeline.StageChangesRequested)

	if orch.shouldEscalate(ctx, workItemID, ticket) {
		return
	}

	engineerName := orch.NameForRole(engineerRole)
	prLink := orch.cfg.Links.PRLink(prURL)
	orch.postAsRole(ctx, "reviews",
		fmt.Sprintf("%s, changes requested on %s — review comments are on the PR. %s", engineerName, ticket, prLink),
		agent.RoleReviewer)

	orch.startFixUp(ctx, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) startFixUp(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	engineerAgent, ok := orch.agents[engineerRole]
	if !ok {
		log.Printf("no agent for %s to address review feedback", engineerRole)
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageWorking)

	_ = orch.checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         engineerRole,
		Ticket:        ticket,
		Status:        coordination.StatusWorking,
		FilesTouching: []string{},
		Message:       fmt.Sprintf("fixing up %s", ticket),
	})
	defer func() { _ = orch.checkIns.SetIdle(ctx, engineerRole) }()

	engineerName := orch.NameForRole(engineerRole)
	log.Printf("fix-up: %s starting fix-up session for %s (%s)", engineerName, ticket, prURL)

	// Narrate start — triggers conversation engine so team can react.
	ticketLink := orch.cfg.Links.TicketLink(ticket)
	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Picking up the review feedback on %s — reading through the comments now.", ticketLink),
		engineerRole)

	prompt := BuildFixUpPrompt(prURL, ticket)

	result, err := engineerAgent.ExecuteTask(ctx, prompt, nil, orch.cfg.TargetRepoDir)
	if err != nil {
		log.Printf("fix-up session failed for %s on %s: %v", engineerRole, ticket, err)
		orch.postAsRole(ctx, "engineering",
			fmt.Sprintf("Hit a snag fixing up %s — will need another pass.", ticketLink),
			engineerRole)
		return
	}

	log.Printf("fix-up: %s completed fix-up for %s", engineerName, ticket)

	// Narrate completion — other agents see progress.
	prLink := orch.cfg.Links.PRLink(prURL)
	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Addressed the review comments on %s — %s. Pushed and ready for re-review.", ticketLink, prLink),
		engineerRole)

	_ = result

	// Re-review: reviewer specifically checks their previous comments.
	orch.startReReview(ctx, prURL, ticket, workItemID, engineerRole)
}

// forceApproval ensures the reviewer's GitHub approval is actually submitted.
func (orch *Orchestrator) startReReview(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		log.Printf("no reviewer agent for re-review")
		return
	}

	log.Printf("re-review: starting re-review of %s for %s", ticket, prURL)
	orch.advancePipeline(ctx, workItemID, pipeline.StageReviewing)
	orch.runReview(ctx, reviewer, prURL, ticket, workItemID, engineerRole, true)
	log.Printf("re-review: completed re-review of %s", ticket)
}
