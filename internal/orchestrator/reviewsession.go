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
	prNum := ExtractPRNumber(prURL)

	mergeAgent := orch.agents[agent.RolePM]
	if mergeAgent == nil {
		return
	}

	// Step 1: Verify review approval — single-purpose prompt.
	approvalStatus := orch.checkApprovalStatus(ctx, mergeAgent, prNum)
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
	if !orch.executeMerge(ctx, mergeAgent, prNum, ticket, engineerRole) {
		return
	}

	// Step 3: Verify the PR is actually merged (already existed).
	if !orch.verifyMerged(ctx, mergeAgent, prNum) {
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

// checkApprovalStatus runs a single-purpose prompt to verify the PR's
// GitHub review decision. Returns approvalStatusApproved, approvalStatusNotApproved, or approvalStatusError.
func (orch *Orchestrator) checkApprovalStatus(ctx context.Context, mergeAgent *agent.Agent, prNum string) string {
	prompt := fmt.Sprintf("Run: gh pr view %s --json reviewDecision --jq .reviewDecision — respond with ONLY the output", prNum)

	result, err := mergeAgent.DirectSession(ctx, prompt)
	if err != nil {
		return approvalStatusError
	}

	upper := strings.ToUpper(result.Transcript)

	// Check for rejection signals before approval — "NOT_APPROVED"
	// contains "APPROVED" as a substring, so order matters.
	rejectionSignals := []string{"NOT_APPROVED", "REVIEW_REQUIRED", "CHANGES_REQUESTED", "PENDING"}
	for _, signal := range rejectionSignals {
		if strings.Contains(upper, signal) {
			return approvalStatusNotApproved
		}
	}

	if strings.Contains(upper, "APPROVED") {
		return approvalStatusApproved
	}

	return approvalStatusNotApproved
}

// CheckApprovalStatusForTest exports checkApprovalStatus for testing.
func (orch *Orchestrator) CheckApprovalStatusForTest(ctx context.Context, mergeAgent *agent.Agent, prNum string) string {
	return orch.checkApprovalStatus(ctx, mergeAgent, prNum)
}

// executeMerge runs a single-purpose prompt to squash-merge the PR.
// Returns true if the merge command succeeded.
func (orch *Orchestrator) executeMerge(ctx context.Context, mergeAgent *agent.Agent, prNum, ticket string, engineerRole agent.Role) bool {
	prompt := fmt.Sprintf("Run: gh pr merge %s --squash --delete-branch — respond with ONLY 'done' or the error message", prNum)

	result, err := mergeAgent.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("merge failed for %s: %v", ticket, err)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s approved but merge failed — needs manual merge", ticket),
			agent.RolePM)
		return false
	}

	upper := strings.ToUpper(result.Transcript)
	if strings.Contains(upper, "CI") && strings.Contains(upper, "FAIL") {
		log.Printf("merge blocked for %s: CI failing", ticket)
		engineerName := orch.NameForRole(engineerRole)
		orch.postAsRole(ctx, "reviews",
			fmt.Sprintf("%s is approved but CI is failing — %s, can you fix and push?", ticket, engineerName),
			agent.RolePM)
		return false
	}

	return true
}

// ExecuteMergeForTest exports executeMerge for testing.
func (orch *Orchestrator) ExecuteMergeForTest(ctx context.Context, mergeAgent *agent.Agent, prNum, ticket string, engineerRole agent.Role) bool {
	return orch.executeMerge(ctx, mergeAgent, prNum, ticket, engineerRole)
}

// VerifyMergedForTest exports verifyMerged for testing.
func (orch *Orchestrator) VerifyMergedForTest(ctx context.Context, verifyAgent *agent.Agent, prNum string) bool {
	return orch.verifyMerged(ctx, verifyAgent, prNum)
}

// verifyMerged checks the actual GitHub PR state to confirm it was merged.
// Never trust an agent's claim — always verify.
func (orch *Orchestrator) verifyMerged(ctx context.Context, verifyAgent *agent.Agent, prNum string) bool {
	prompt := fmt.Sprintf("Run this command and respond with ONLY the output, nothing else:\ngh pr view %s --json state --jq .state", prNum)

	result, err := verifyAgent.DirectSession(ctx, prompt)
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToUpper(result.Transcript), "MERGED")
}

func (orch *Orchestrator) retryApproval(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		return
	}

	prNum := ExtractPRNumber(prURL)
	reviewerName := orch.NameForRole(agent.RoleReviewer)
	engineerName := orch.NameForRole(engineerRole)

	orch.postAsRole(ctx, "reviews",
		fmt.Sprintf("%s is approved but the GitHub review wasn't submitted — %s, re-submitting now. %s, hang tight.",
			ticket, reviewerName, engineerName),
		agent.RolePM)

	// Step 1: Single-purpose — submit the approval.
	approvePrompt := fmt.Sprintf("Run: gh pr review %s --approve --body 'Approved' — respond with ONLY 'done' or the error", prNum)

	_, err := reviewer.DirectSession(ctx, approvePrompt)
	if err != nil {
		log.Printf("retry approval failed for %s: %v", ticket, err)
		return
	}

	// Step 2: Single-purpose — verify the approval landed.
	verifyPrompt := fmt.Sprintf("Run this and respond with ONLY the output:\ngh pr view %s --json reviewDecision --jq .reviewDecision", prNum)

	verifyResult, verifyErr := reviewer.DirectSession(ctx, verifyPrompt)
	if verifyErr != nil {
		log.Printf("approval verification failed for %s: %v", ticket, verifyErr)
		return
	}

	if !strings.Contains(strings.ToUpper(verifyResult.Transcript), "APPROVED") {
		log.Printf("retry approval did not land for %s: %s", ticket, verifyResult.Transcript)
		return
	}

	log.Printf("retry approval verified for %s, retrying merge", ticket)
	orch.mergeAfterRetry(ctx, prURL, ticket, workItemID, engineerRole)
}

// MergeAfterRetryForTest exports mergeAfterRetry for testing.
func (orch *Orchestrator) MergeAfterRetryForTest(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	orch.mergeAfterRetry(ctx, prURL, ticket, workItemID, engineerRole)
}

// mergeAfterRetry is mergeAndComplete without the retryApproval fallback.
// Prevents infinite recursion: retryApproval → mergeAndComplete → retryApproval.
func (orch *Orchestrator) mergeAfterRetry(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	prNum := ExtractPRNumber(prURL)

	mergeAgent := orch.agents[agent.RolePM]
	if mergeAgent == nil {
		return
	}

	approvalStatus := orch.checkApprovalStatus(ctx, mergeAgent, prNum)
	if approvalStatus != approvalStatusApproved {
		log.Printf("merge blocked for %s after retry: still not approved (%s)", ticket, approvalStatus)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s re-approval attempted but still not approved — needs manual merge", ticket),
			agent.RolePM)
		return
	}

	if !orch.executeMerge(ctx, mergeAgent, prNum, ticket, engineerRole) {
		return
	}

	if !orch.verifyMerged(ctx, mergeAgent, prNum) {
		log.Printf("merge verification failed for %s after retry", ticket)
		orch.announceAsRole(ctx, "reviews",
			fmt.Sprintf("%s merge attempted after retry but PR is still open — needs manual merge", ticket),
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

func (orch *Orchestrator) handleChangesRequested(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role, reviewSummary string) {
	orch.advancePipeline(ctx, workItemID, pipeline.StageChangesRequested)

	if orch.shouldEscalate(ctx, workItemID, ticket) {
		return
	}

	engineerName := orch.NameForRole(engineerRole)
	orch.postAsRole(ctx, "reviews",
		fmt.Sprintf("%s, changes requested on %s: %s. Fixing now.", engineerName, ticket, reviewSummary),
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

	prompt := BuildFixUpPrompt(prURL, ticket)

	result, err := engineerAgent.ExecuteTask(ctx, prompt, nil, orch.cfg.TargetRepoDir)
	if err != nil {
		log.Printf("fix-up session failed for %s on %s: %v", engineerRole, ticket, err)
		return
	}

	_ = result

	// Re-review: reviewer specifically checks their previous comments.
	orch.startReReview(ctx, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) startReReview(ctx context.Context, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	reviewer, ok := orch.agents[agent.RoleReviewer]
	if !ok {
		log.Printf("no reviewer agent for re-review")
		return
	}

	orch.advancePipeline(ctx, workItemID, pipeline.StageReviewing)
	orch.runReview(ctx, reviewer, prURL, ticket, workItemID, engineerRole, true)
}
